// Copyright 2024 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package statetest

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// ErrNoCorpus is returned when the corpus is empty
var ErrNoCorpus = errors.New("corpus is empty")

// PriorityInput represents a fuzz input with coverage metadata
type PriorityInput struct {
	Data           []byte    // The actual test data
	Priority       int       // Current priority (decays over time)
	BasePriority   int       // Original priority at discovery
	CoverageDelta  float64   // How much coverage this input added
	DiscoveredAt   time.Time // When this input was discovered
	ParentStrategy string    // Which mutation strategy created this
	PickCount      int64     // Times this input was picked
	LastPickTime   time.Time // When last picked
	dataHash       string    // SHA256 hash for O(1) lookup (internal)
}

// PriorityHeap implements heap.Interface for priority-based scheduling
// NOTE: Kept for backward compatibility but no longer used by CoverageCorpus
type PriorityHeap []*PriorityInput

func (h PriorityHeap) Len() int { return len(h) }

// Less returns true if item i has higher priority than item j
// We want max-heap behavior (highest priority first)
func (h PriorityHeap) Less(i, j int) bool { return h[i].Priority > h[j].Priority }

func (h PriorityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *PriorityHeap) Push(x any) {
	item := x.(*PriorityInput)
	*h = append(*h, item)
}

func (h *PriorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // Avoid memory leak
	*h = old[0 : n-1]
	return item
}

// CoverageCorpus manages inputs with coverage-guided prioritization.
// It implements the mutations.CorpusProvider interface for splicing support.
type CoverageCorpus struct {
	mu sync.RWMutex

	// HP queue - simple slice with retention (items stay in queue after Pop)
	hpQueue       []*PriorityInput          // All high-priority inputs
	hpIndex       map[string]*PriorityInput // dataHash -> item for O(1) lookup
	totalPriority int64                     // Running sum for weighted selection

	normalSeeds [][]byte // Original seed corpus (round-robin)
	seedIndex   int      // Current position in normalSeeds

	// Splicing corpus - all inputs that found coverage
	splicingPool [][]byte

	// Random number generator (protected by mutex)
	rng *rand.Rand

	// Configuration
	maxQueueSize   int     // Maximum high priority queue size (0 = unlimited)
	highPriorityP  float64 // Probability of picking from high priority queue
	maxSplicingLen int     // Maximum splicing pool size

	// Retention config
	decayRate      float64 // Priority decay per pick (default: 0.99)
	minPriority    int     // Cull threshold (default: 1)
	cullInterval   int     // Cull every N pops (default: 1000)
	popsSinceCull  int     // Counter for inline culling
	minPicksToCull int     // Minimum picks before item can be culled (default: 10)

	// Stats
	maxCoverage      float64   // Maximum coverage achieved
	totalInputsAdded int64     // Total inputs added to high priority
	lastFindTime     time.Time // Time of last coverage find
	totalCulled      int64     // Total items ever culled

	// Pick tracking (atomic counters)
	hpPicks   int64 // Times high priority queue was selected
	seedPicks int64 // Times seeds were selected (round-robin)

	// Splice donor tracking
	spliceCoverageDonors int64 // Times splicingPool was selected as donor
	spliceSeedDonors     int64 // Times seeds were selected as donor
}

// CoverageCorpusOption is a functional option for configuring CoverageCorpus
type CoverageCorpusOption func(*CoverageCorpus)

// WithMaxQueueSize sets the maximum high priority queue size
func WithMaxQueueSize(size int) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		c.maxQueueSize = size
	}
}

// WithHighPriorityProbability sets the probability of picking from high priority queue
func WithHighPriorityProbability(p float64) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		if p >= 0 && p <= 1 {
			c.highPriorityP = p
		}
	}
}

// WithMaxSplicingPoolSize sets the maximum splicing pool size
func WithMaxSplicingPoolSize(size int) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		c.maxSplicingLen = size
	}
}

// WithDecayRate sets the priority decay rate per pick (0 < rate <= 1)
func WithDecayRate(rate float64) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		if rate > 0 && rate <= 1 {
			c.decayRate = rate
		}
	}
}

// WithMinPriority sets the minimum priority threshold for culling
func WithMinPriority(min int) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		if min >= 0 {
			c.minPriority = min
		}
	}
}

// WithCullInterval sets how often culling occurs (every N pops)
func WithCullInterval(interval int) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		if interval > 0 {
			c.cullInterval = interval
		}
	}
}

// WithMinPicksToCull sets minimum picks before item becomes eligible for culling
func WithMinPicksToCull(picks int) CoverageCorpusOption {
	return func(c *CoverageCorpus) {
		if picks >= 0 {
			c.minPicksToCull = picks
		}
	}
}

// NewCoverageCorpus creates a corpus with initial seeds
func NewCoverageCorpus(seeds [][]byte, opts ...CoverageCorpusOption) *CoverageCorpus {
	c := &CoverageCorpus{
		hpQueue:        make([]*PriorityInput, 0),
		hpIndex:        make(map[string]*PriorityInput),
		totalPriority:  0,
		normalSeeds:    make([][]byte, len(seeds)),
		splicingPool:   make([][]byte, 0, 1000),
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
		maxQueueSize:   10000, // Default max queue size
		highPriorityP:  0.8,   // 80% chance to pick high priority
		maxSplicingLen: 10000, // Default max splicing pool
		decayRate:      0.99,  // Slow decay: ~460 picks to reach minPriority from 100
		minPriority:    1,     // Only cull truly exhausted items
		cullInterval:   1000,  // Default cull every 1000 pops
		minPicksToCull: 10,    // Item must be picked 10+ times before culling eligible
		popsSinceCull:  0,
		lastFindTime:   time.Now(),
		totalCulled:    0,
	}

	// Copy seeds to prevent external modification
	for i, seed := range seeds {
		c.normalSeeds[i] = make([]byte, len(seed))
		copy(c.normalSeeds[i], seed)
	}

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Pop returns the next input to fuzz (high priority first, then round-robin seeds).
// Items remain in the HP queue after being picked (retention).
// Returns nil if corpus is completely empty.
func (c *CoverageCorpus) Pop() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Inline culling check
	c.popsSinceCull++
	if c.popsSinceCull >= c.cullInterval {
		c.cullLocked()
		c.popsSinceCull = 0
	}

	// Decide whether to use high priority queue
	useHighPriority := len(c.hpQueue) > 0 && c.rng.Float64() < c.highPriorityP

	if useHighPriority {
		c.hpPicks++
		item := c.weightedSelectLocked()
		if item != nil {
			// Update pick stats
			item.PickCount++
			item.LastPickTime = time.Now()

			// Apply decay
			c.applyDecayLocked(item)

			// Return a copy to prevent external modification
			result := make([]byte, len(item.Data))
			copy(result, item.Data)
			return result
		}
	}

	// Fall back to normal seeds (round-robin)
	if len(c.normalSeeds) == 0 {
		return nil
	}

	c.seedPicks++
	input := c.normalSeeds[c.seedIndex%len(c.normalSeeds)]
	c.seedIndex++

	// Return a copy
	result := make([]byte, len(input))
	copy(result, input)
	return result
}

// weightedSelectLocked selects an item with probability proportional to priority.
// MUST be called with c.mu held.
func (c *CoverageCorpus) weightedSelectLocked() *PriorityInput {
	if len(c.hpQueue) == 0 || c.totalPriority <= 0 {
		return nil
	}

	// Weighted random selection using running totalPriority
	target := c.rng.Int63n(c.totalPriority)
	var cumulative int64
	for _, item := range c.hpQueue {
		cumulative += int64(item.Priority)
		if cumulative > target {
			return item
		}
	}

	// Fallback (shouldn't happen if totalPriority is accurate)
	return c.hpQueue[len(c.hpQueue)-1]
}

// applyDecayLocked reduces an item's priority. MUST be called with c.mu held.
func (c *CoverageCorpus) applyDecayLocked(item *PriorityInput) {
	oldPriority := item.Priority
	newPriority := int(float64(item.Priority) * c.decayRate)
	if newPriority < c.minPriority {
		newPriority = c.minPriority
	}

	// Update running sum
	c.totalPriority -= int64(oldPriority)
	c.totalPriority += int64(newPriority)
	item.Priority = newPriority
}

// cullLocked removes items below minPriority that have been picked enough times.
// MUST be called with c.mu held.
func (c *CoverageCorpus) cullLocked() int {
	if len(c.hpQueue) == 0 {
		return 0
	}

	newQueue := make([]*PriorityInput, 0, len(c.hpQueue))
	culled := 0

	for _, item := range c.hpQueue {
		// Keep item if: priority is above threshold OR hasn't been picked enough times
		if item.Priority > c.minPriority || item.PickCount < int64(c.minPicksToCull) {
			newQueue = append(newQueue, item)
		} else {
			// Remove from index
			delete(c.hpIndex, item.dataHash)
			c.totalPriority -= int64(item.Priority)
			culled++
		}
	}

	c.hpQueue = newQueue
	c.totalCulled += int64(culled)
	return culled
}

// boostPriorityLocked increases an existing item's priority. MUST be called with c.mu held.
func (c *CoverageCorpus) boostPriorityLocked(item *PriorityInput, coverageDelta float64) {
	oldPriority := item.Priority
	boost := int(coverageDelta * 1000000)
	newPriority := item.BasePriority + boost

	c.totalPriority -= int64(oldPriority)
	c.totalPriority += int64(newPriority)

	item.Priority = newPriority
	item.CoverageDelta += coverageDelta
}

// replaceLowestLocked replaces the lowest priority item with a new one.
// MUST be called with c.mu held.
func (c *CoverageCorpus) replaceLowestLocked(newItem *PriorityInput) {
	if len(c.hpQueue) == 0 {
		return
	}

	// Find the lowest priority item
	lowestIdx := 0
	lowestPriority := c.hpQueue[0].Priority
	for i, item := range c.hpQueue {
		if item.Priority < lowestPriority {
			lowestIdx = i
			lowestPriority = item.Priority
		}
	}

	// Only replace if new item has higher priority
	if newItem.Priority > lowestPriority {
		oldItem := c.hpQueue[lowestIdx]

		// Update totalPriority
		c.totalPriority -= int64(oldItem.Priority)
		c.totalPriority += int64(newItem.Priority)

		// Remove old from index, add new
		delete(c.hpIndex, oldItem.dataHash)
		c.hpIndex[newItem.dataHash] = newItem

		// Replace in slice
		c.hpQueue[lowestIdx] = newItem
	}
}

// AddHighPriority adds an input that found new coverage
func (c *CoverageCorpus) AddHighPriority(input *PriorityInput) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Compute data hash
	hash := sha256.Sum256(input.Data)
	dataHash := hex.EncodeToString(hash[:])

	// Check if already exists (boost priority instead of adding duplicate)
	if existing, ok := c.hpIndex[dataHash]; ok {
		c.boostPriorityLocked(existing, input.CoverageDelta)
		return
	}

	// Make a copy of the data
	dataCopy := make([]byte, len(input.Data))
	copy(dataCopy, input.Data)

	// Create new item
	newItem := &PriorityInput{
		Data:           dataCopy,
		Priority:       input.Priority,
		BasePriority:   input.Priority,
		CoverageDelta:  input.CoverageDelta,
		DiscoveredAt:   input.DiscoveredAt,
		ParentStrategy: input.ParentStrategy,
		PickCount:      0,
		LastPickTime:   time.Time{},
		dataHash:       dataHash,
	}

	// Check queue size limit
	if c.maxQueueSize > 0 && len(c.hpQueue) >= c.maxQueueSize {
		// Replace lowest priority item
		c.replaceLowestLocked(newItem)
	} else {
		// Add to queue
		c.hpQueue = append(c.hpQueue, newItem)
		c.hpIndex[dataHash] = newItem
		c.totalPriority += int64(newItem.Priority)
	}

	// Add to splicing pool
	if c.maxSplicingLen == 0 || len(c.splicingPool) < c.maxSplicingLen {
		c.splicingPool = append(c.splicingPool, dataCopy)
	} else {
		// Replace a random item
		c.splicingPool[c.rng.Intn(len(c.splicingPool))] = dataCopy
	}

	// Update stats
	c.totalInputsAdded++
	c.lastFindTime = input.DiscoveredAt
	newCov := c.maxCoverage + input.CoverageDelta
	if newCov > c.maxCoverage {
		c.maxCoverage = newCov
	}
}

// GetRandomInput implements mutations.CorpusProvider for splicing strategy.
// Uses weighted sampling: 70% splicingPool, 30% seeds when both are available.
// This ensures expert EEST seeds remain in play even after coverage-finding inputs exist.
func (c *CoverageCorpus) GetRandomInput() ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hasSplicingPool := len(c.splicingPool) > 0
	hasSeeds := len(c.normalSeeds) > 0

	// Handle cases where only one source is available
	if !hasSplicingPool && !hasSeeds {
		return nil, ErrNoCorpus
	}

	var input []byte
	if hasSplicingPool && hasSeeds {
		// Both available: 70% splicingPool, 30% seeds
		if c.rng.Intn(100) < 70 {
			c.spliceCoverageDonors++
			input = c.splicingPool[c.rng.Intn(len(c.splicingPool))]
		} else {
			c.spliceSeedDonors++
			input = c.normalSeeds[c.rng.Intn(len(c.normalSeeds))]
		}
	} else if hasSplicingPool {
		// Only splicingPool available
		c.spliceCoverageDonors++
		input = c.splicingPool[c.rng.Intn(len(c.splicingPool))]
	} else {
		// Only seeds available
		c.spliceSeedDonors++
		input = c.normalSeeds[c.rng.Intn(len(c.normalSeeds))]
	}

	// Return a copy to prevent external modification
	result := make([]byte, len(input))
	copy(result, input)
	return result, nil
}

// GetInputCount implements mutations.CorpusProvider
func (c *CoverageCorpus) GetInputCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.splicingPool) + len(c.normalSeeds)
}

// Stats returns corpus statistics (legacy interface)
func (c *CoverageCorpus) Stats() (highPriorityLen, splicingPoolLen int, maxCov float64, lastFind time.Time) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.hpQueue), len(c.splicingPool), c.maxCoverage, c.lastFindTime
}

// CoverageCorpusStats contains detailed runtime statistics for the coverage corpus
type CoverageCorpusStats struct {
	HPQueueLen   int   // Current high priority queue length
	SplicingLen  int   // Current splicing pool size
	SeedCount    int   // Number of original seeds
	HPPicks      int64 // Times HP queue was selected
	SeedPicks    int64 // Times seeds were selected
	TotalAdded   int64 // Total inputs ever added to HP queue
	MaxCoverage  float64
	LastFindTime time.Time

	// Splice donor tracking
	SpliceCoverageDonors int64 // Times splicingPool was selected as splice donor
	SpliceSeedDonors     int64 // Times seeds was selected as splice donor

	// Retention stats
	TotalPriority int64   // Current sum of all priorities
	TotalCulled   int64   // Total items ever culled
	AvgPickCount  float64 // Average picks per item in queue
}

// FullStats returns detailed corpus statistics
func (c *CoverageCorpus) FullStats() CoverageCorpusStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Compute average pick count
	var avgPickCount float64
	if len(c.hpQueue) > 0 {
		var totalPicks int64
		for _, item := range c.hpQueue {
			totalPicks += item.PickCount
		}
		avgPickCount = float64(totalPicks) / float64(len(c.hpQueue))
	}

	return CoverageCorpusStats{
		HPQueueLen:           len(c.hpQueue),
		SplicingLen:          len(c.splicingPool),
		SeedCount:            len(c.normalSeeds),
		HPPicks:              c.hpPicks,
		SeedPicks:            c.seedPicks,
		TotalAdded:           c.totalInputsAdded,
		MaxCoverage:          c.maxCoverage,
		LastFindTime:         c.lastFindTime,
		SpliceCoverageDonors: c.spliceCoverageDonors,
		SpliceSeedDonors:     c.spliceSeedDonors,
		TotalPriority:        c.totalPriority,
		TotalCulled:          c.totalCulled,
		AvgPickCount:         avgPickCount,
	}
}

// SeedCount returns the number of original seeds
func (c *CoverageCorpus) SeedCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.normalSeeds)
}

// TotalInputsAdded returns the total number of inputs added to high priority
func (c *CoverageCorpus) TotalInputsAdded() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalInputsAdded
}

// AddSeeds adds additional seeds to the corpus
func (c *CoverageCorpus) AddSeeds(seeds [][]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for _, seed := range seeds {
		seedCopy := make([]byte, len(seed))
		copy(seedCopy, seed)
		c.normalSeeds = append(c.normalSeeds, seedCopy)
	}
}

// Clear removes all inputs from the corpus (keeps original seeds)
func (c *CoverageCorpus) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.hpQueue = make([]*PriorityInput, 0)
	c.hpIndex = make(map[string]*PriorityInput)
	c.totalPriority = 0
	c.splicingPool = make([][]byte, 0, 1000)
	c.maxCoverage = 0
	c.totalInputsAdded = 0
	c.totalCulled = 0
	c.popsSinceCull = 0
}
