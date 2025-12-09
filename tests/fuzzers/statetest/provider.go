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
	"errors"
	"sync"
	"sync/atomic"
)

// ErrProviderExhausted is returned when a provider has no more inputs.
var ErrProviderExhausted = errors.New("provider exhausted")

// InputProvider abstracts the source of fuzz inputs.
// This allows swapping between mutation-based, generation-based, or hybrid approaches.
// Implementations must be thread-safe for concurrent worker access.
type InputProvider interface {
	// Next returns the next input to fuzz.
	// Returns:
	//   - data: the state test JSON bytes
	//   - source: identifier for attribution (e.g., "mutation:bytecode", "generation:ecrecover")
	//   - err: error if provider is exhausted or failed
	Next() (data []byte, source string, err error)

	// Feedback provides coverage feedback to the provider.
	// This allows mutation-based providers to prioritize successful inputs.
	// Thread-safe: may be called concurrently from multiple workers.
	Feedback(data []byte, source string, coverageDelta float64)

	// Stats returns provider-specific statistics for reporting.
	// Thread-safe: returns a snapshot of current statistics.
	Stats() ProviderStats

	// Name returns the provider name for logging.
	Name() string
}

// ProviderStats contains metrics for A/B testing comparison.
// All fields are safe for concurrent read after being returned from Stats().
type ProviderStats struct {
	// ProviderName identifies which provider generated these stats.
	ProviderName string

	// TotalInputs is the total number of inputs generated.
	TotalInputs int64

	// CoverageFinds is the number of inputs that found new coverage.
	CoverageFinds int64

	// CumulativeDelta is the sum of all coverage deltas.
	CumulativeDelta float64

	// SourceBreakdown provides per-source (strategy/generator) statistics.
	// Keys are source identifiers like "mutation:bytecode" or "generation:ecrecover".
	SourceBreakdown map[string]*SourceStats
}

// SourceStats tracks per-source (strategy/generator) statistics.
type SourceStats struct {
	// Inputs is the number of inputs from this source.
	Inputs int64

	// CoverageFinds is the number of inputs that found coverage.
	CoverageFinds int64

	// TotalDelta is the sum of coverage deltas from this source.
	TotalDelta float64
}

// FindRate returns the ratio of coverage finds to total inputs.
// Returns 0 if no inputs have been processed.
func (s *SourceStats) FindRate() float64 {
	if s.Inputs == 0 {
		return 0
	}
	return float64(s.CoverageFinds) / float64(s.Inputs)
}

// AvgDelta returns the average coverage delta per find.
// Returns 0 if no coverage finds.
func (s *SourceStats) AvgDelta() float64 {
	if s.CoverageFinds == 0 {
		return 0
	}
	return s.TotalDelta / float64(s.CoverageFinds)
}

// Clone returns a deep copy of SourceStats.
func (s *SourceStats) Clone() *SourceStats {
	return &SourceStats{
		Inputs:        s.Inputs,
		CoverageFinds: s.CoverageFinds,
		TotalDelta:    s.TotalDelta,
	}
}

// statsTracker provides thread-safe statistics tracking for providers.
// It can be embedded in provider implementations.
type statsTracker struct {
	mu              sync.RWMutex
	totalInputs     int64
	coverageFinds   int64
	cumulativeDelta float64
	sourceBreakdown map[string]*SourceStats
}

// newStatsTracker creates a new stats tracker.
func newStatsTracker() *statsTracker {
	return &statsTracker{
		sourceBreakdown: make(map[string]*SourceStats),
	}
}

// recordInput records that an input was generated from the given source.
func (t *statsTracker) recordInput(source string) {
	atomic.AddInt64(&t.totalInputs, 1)

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.sourceBreakdown[source] == nil {
		t.sourceBreakdown[source] = &SourceStats{}
	}
	t.sourceBreakdown[source].Inputs++
}

// recordFeedback records coverage feedback for a source.
func (t *statsTracker) recordFeedback(source string, coverageDelta float64) {
	if coverageDelta > 0 {
		atomic.AddInt64(&t.coverageFinds, 1)
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	t.cumulativeDelta += coverageDelta

	if t.sourceBreakdown[source] == nil {
		t.sourceBreakdown[source] = &SourceStats{}
	}
	t.sourceBreakdown[source].TotalDelta += coverageDelta
	if coverageDelta > 0 {
		t.sourceBreakdown[source].CoverageFinds++
	}
}

// stats returns a snapshot of current statistics.
func (t *statsTracker) stats(providerName string) ProviderStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	breakdown := make(map[string]*SourceStats, len(t.sourceBreakdown))
	for source, stats := range t.sourceBreakdown {
		breakdown[source] = stats.Clone()
	}

	return ProviderStats{
		ProviderName:    providerName,
		TotalInputs:     atomic.LoadInt64(&t.totalInputs),
		CoverageFinds:   atomic.LoadInt64(&t.coverageFinds),
		CumulativeDelta: t.cumulativeDelta,
		SourceBreakdown: breakdown,
	}
}

// FindRate returns the overall find rate.
func (p *ProviderStats) FindRate() float64 {
	if p.TotalInputs == 0 {
		return 0
	}
	return float64(p.CoverageFinds) / float64(p.TotalInputs)
}

// AvgDeltaPerFind returns the average coverage delta per find.
func (p *ProviderStats) AvgDeltaPerFind() float64 {
	if p.CoverageFinds == 0 {
		return 0
	}
	return p.CumulativeDelta / float64(p.CoverageFinds)
}

// TopSources returns the top N sources by coverage finds.
func (p *ProviderStats) TopSources(n int) []string {
	if len(p.SourceBreakdown) == 0 {
		return nil
	}

	// Collect and sort by finds
	type sourceFind struct {
		name  string
		finds int64
	}
	sources := make([]sourceFind, 0, len(p.SourceBreakdown))
	for name, stats := range p.SourceBreakdown {
		sources = append(sources, sourceFind{name, stats.CoverageFinds})
	}

	// Simple insertion sort (typically small N)
	for i := 1; i < len(sources); i++ {
		for j := i; j > 0 && sources[j].finds > sources[j-1].finds; j-- {
			sources[j], sources[j-1] = sources[j-1], sources[j]
		}
	}

	// Return top N names
	if n > len(sources) {
		n = len(sources)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = sources[i].name
	}
	return result
}
