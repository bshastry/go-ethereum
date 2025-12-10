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
	"container/heap"
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
	Priority       int       // Higher = more important (scaled from coverage delta)
	CoverageDelta  float64   // How much coverage this input added
	DiscoveredAt   time.Time // When this input was discovered
	ParentStrategy string    // Which mutation strategy created this
	index          int       // Heap index for efficient updates
}

// PriorityHeap implements heap.Interface for priority-based scheduling
type PriorityHeap []*PriorityInput

func (h PriorityHeap) Len() int { return len(h) }

// Less returns true if item i has higher priority than item j
// We want max-heap behavior (highest priority first)
func (h PriorityHeap) Less(i, j int) bool { return h[i].Priority > h[j].Priority }

func (h PriorityHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}

func (h *PriorityHeap) Push(x any) {
	n := len(*h)
	item := x.(*PriorityInput)
	item.index = n
	*h = append(*h, item)
}

func (h *PriorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil  // Avoid memory leak
	item.index = -1 // Mark as removed
	*h = old[0 : n-1]
	return item
}

// CoverageCorpus manages inputs with coverage-guided prioritization.
// It implements the mutations.CorpusProvider interface for splicing support.
type CoverageCorpus struct {
	mu           sync.RWMutex
	highPriority PriorityHeap // Heap ordered by priority (coverage delta)
	normalSeeds  [][]byte     // Original seed corpus (round-robin)
	seedIndex    int          // Current position in normalSeeds

	// Splicing corpus - all inputs that found coverage
	splicingPool [][]byte

	// Random number generator (protected by mutex)
	rng *rand.Rand

	// Configuration
	maxQueueSize   int     // Maximum high priority queue size (0 = unlimited)
	highPriorityP  float64 // Probability of picking from high priority queue
	maxSplicingLen int     // Maximum splicing pool size

	// Stats
	maxCoverage      float64   // Maximum coverage achieved
	totalInputsAdded int64     // Total inputs added to high priority
	lastFindTime     time.Time // Time of last coverage find

	// Pick tracking (atomic counters)
	hpPicks   int64 // Times high priority queue was selected
	seedPicks int64 // Times seeds were selected (round-robin)
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

// NewCoverageCorpus creates a corpus with initial seeds
func NewCoverageCorpus(seeds [][]byte, opts ...CoverageCorpusOption) *CoverageCorpus {
	c := &CoverageCorpus{
		normalSeeds:    make([][]byte, len(seeds)),
		splicingPool:   make([][]byte, 0, 1000),
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
		maxQueueSize:   10000,  // Default max queue size
		highPriorityP:  0.8,    // 80% chance to pick high priority
		maxSplicingLen: 10000,  // Default max splicing pool
		lastFindTime:   time.Now(),
	}

	// Copy seeds to prevent external modification
	for i, seed := range seeds {
		c.normalSeeds[i] = make([]byte, len(seed))
		copy(c.normalSeeds[i], seed)
	}

	// Initialize the heap
	heap.Init(&c.highPriority)

	// Apply options
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Pop returns the next input to fuzz (high priority first, then round-robin seeds).
// Returns nil if corpus is completely empty.
func (c *CoverageCorpus) Pop() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Decide whether to use high priority queue
	useHighPriority := len(c.highPriority) > 0 && c.rng.Float64() < c.highPriorityP

	if useHighPriority {
		c.hpPicks++
		item := heap.Pop(&c.highPriority).(*PriorityInput)
		// Return a copy to prevent external modification
		result := make([]byte, len(item.Data))
		copy(result, item.Data)
		return result
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

// AddHighPriority adds an input that found new coverage
func (c *CoverageCorpus) AddHighPriority(input *PriorityInput) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Make a copy of the data
	dataCopy := make([]byte, len(input.Data))
	copy(dataCopy, input.Data)
	input.Data = dataCopy

	// Add to priority queue
	if c.maxQueueSize == 0 || len(c.highPriority) < c.maxQueueSize {
		heap.Push(&c.highPriority, input)
	} else {
		// Queue is full - only add if this has higher priority than minimum
		// Since this is a max-heap, we need to find the minimum manually
		// For simplicity, we'll just pop the last item if this has higher priority
		// than a random existing item
		if len(c.highPriority) > 0 {
			// Replace a random item with probability based on priority difference
			randomIdx := c.rng.Intn(len(c.highPriority))
			if input.Priority > c.highPriority[randomIdx].Priority {
				// Remove the old item and add new one
				old := c.highPriority[randomIdx]
				c.highPriority[randomIdx] = input
				input.index = randomIdx
				heap.Fix(&c.highPriority, randomIdx)
				old.index = -1
			}
		}
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

// GetRandomInput implements mutations.CorpusProvider for splicing strategy
func (c *CoverageCorpus) GetRandomInput() ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Prefer splicing pool if available (coverage-finding inputs)
	if len(c.splicingPool) > 0 {
		input := c.splicingPool[c.rng.Intn(len(c.splicingPool))]
		result := make([]byte, len(input))
		copy(result, input)
		return result, nil
	}

	// Fall back to normal seeds
	if len(c.normalSeeds) == 0 {
		return nil, ErrNoCorpus
	}

	input := c.normalSeeds[c.rng.Intn(len(c.normalSeeds))]
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
	return len(c.highPriority), len(c.splicingPool), c.maxCoverage, c.lastFindTime
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
}

// FullStats returns detailed corpus statistics
func (c *CoverageCorpus) FullStats() CoverageCorpusStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CoverageCorpusStats{
		HPQueueLen:   len(c.highPriority),
		SplicingLen:  len(c.splicingPool),
		SeedCount:    len(c.normalSeeds),
		HPPicks:      c.hpPicks,
		SeedPicks:    c.seedPicks,
		TotalAdded:   c.totalInputsAdded,
		MaxCoverage:  c.maxCoverage,
		LastFindTime: c.lastFindTime,
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

	c.highPriority = make(PriorityHeap, 0)
	heap.Init(&c.highPriority)
	c.splicingPool = make([][]byte, 0, 1000)
	c.maxCoverage = 0
	c.totalInputsAdded = 0
}
