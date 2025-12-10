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

//go:build generators

package generators

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"
)

// ErrNoGenerators is returned when no generators are available.
var ErrNoGenerators = errors.New("no generators available")

// GeneratorProvider uses only generators (no corpus, no mutation).
// This is used for A/B testing to measure pure generation effectiveness.
type GeneratorProvider struct {
	registry   *Registry
	generators []Generator
	weights    []int
	totalW     int
	fork       string

	mu  sync.Mutex
	rng *rand.Rand

	// Statistics
	totalInputs     int64
	coverageFinds   int64
	cumulativeDelta float64
	sourceStats     map[string]*GeneratorStats
	statsMu         sync.RWMutex
}

// NewGeneratorProvider creates a provider that uses only generators.
func NewGeneratorProvider(registry *Registry, fork string) *GeneratorProvider {
	gens := registry.ForFork(fork)

	weights := make([]int, len(gens))
	totalW := 0
	for i, g := range gens {
		weights[i] = g.Weight()
		totalW += weights[i]
	}

	return &GeneratorProvider{
		registry:    registry,
		generators:  gens,
		weights:     weights,
		totalW:      totalW,
		fork:        fork,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		sourceStats: make(map[string]*GeneratorStats),
	}
}

// NewGeneratorProviderWithNames creates a provider with specific generators.
func NewGeneratorProviderWithNames(registry *Registry, fork string, names []string) *GeneratorProvider {
	var gens []Generator
	for _, name := range names {
		if g, err := registry.Get(name); err == nil {
			// Check if generator supports the fork
			info, _ := registry.Info(name)
			if info != nil && info.ForkSupported(fork) {
				gens = append(gens, g)
			}
		}
	}

	weights := make([]int, len(gens))
	totalW := 0
	for i, g := range gens {
		weights[i] = g.Weight()
		totalW += weights[i]
	}

	return &GeneratorProvider{
		registry:    registry,
		generators:  gens,
		weights:     weights,
		totalW:      totalW,
		fork:        fork,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
		sourceStats: make(map[string]*GeneratorStats),
	}
}

// Name returns the provider name.
func (p *GeneratorProvider) Name() string {
	return "generation"
}

// Next returns a newly generated test from a randomly selected generator.
func (p *GeneratorProvider) Next() ([]byte, string, error) {
	if len(p.generators) == 0 {
		return nil, "", ErrNoGenerators
	}

	p.mu.Lock()
	// Weighted random selection
	r := p.rng.Intn(p.totalW)
	cumulative := 0
	var selected Generator
	for i, g := range p.generators {
		cumulative += p.weights[i]
		if r < cumulative {
			selected = g
			break
		}
	}
	p.mu.Unlock()

	if selected == nil {
		selected = p.generators[0] // Fallback
	}

	// Generate the test
	data, err := selected.Generate(p.fork)
	if err != nil {
		return nil, "", err
	}

	atomic.AddInt64(&p.totalInputs, 1)

	source := "generation:" + selected.Name()

	// Record input in stats
	p.statsMu.Lock()
	if p.sourceStats[source] == nil {
		p.sourceStats[source] = &GeneratorStats{Name: selected.Name()}
	}
	p.sourceStats[source].Generated++
	p.statsMu.Unlock()

	return data, source, nil
}

// Feedback records coverage feedback for a generated test.
// Note: In pure generation mode, we don't feed back to a corpus.
func (p *GeneratorProvider) Feedback(data []byte, source string, coverageDelta float64) {
	if coverageDelta > 0 {
		atomic.AddInt64(&p.coverageFinds, 1)
	}

	p.statsMu.Lock()
	defer p.statsMu.Unlock()

	p.cumulativeDelta += coverageDelta

	if p.sourceStats[source] == nil {
		p.sourceStats[source] = &GeneratorStats{Name: source}
	}
	if coverageDelta > 0 {
		p.sourceStats[source].CoverageFinds++
	}
	p.sourceStats[source].TotalDelta += coverageDelta
}

// Stats returns current provider statistics.
func (p *GeneratorProvider) Stats() ProviderStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()

	breakdown := make(map[string]*SourceStats, len(p.sourceStats))
	for source, gs := range p.sourceStats {
		breakdown[source] = &SourceStats{
			Inputs:        gs.Generated,
			CoverageFinds: gs.CoverageFinds,
			TotalDelta:    gs.TotalDelta,
		}
	}

	return ProviderStats{
		ProviderName:    p.Name(),
		TotalInputs:     atomic.LoadInt64(&p.totalInputs),
		CoverageFinds:   atomic.LoadInt64(&p.coverageFinds),
		CumulativeDelta: p.cumulativeDelta,
		SourceBreakdown: breakdown,
	}
}

// GeneratorCount returns the number of available generators.
func (p *GeneratorProvider) GeneratorCount() int {
	return len(p.generators)
}

// GeneratorNames returns the names of available generators.
func (p *GeneratorProvider) GeneratorNames() []string {
	names := make([]string, len(p.generators))
	for i, g := range p.generators {
		names[i] = g.Name()
	}
	return names
}

// Fork returns the target fork.
func (p *GeneratorProvider) Fork() string {
	return p.fork
}

// ProviderStats mirrors the stats structure from the parent package.
// This avoids import cycles while providing compatible stats.
type ProviderStats struct {
	ProviderName    string
	TotalInputs     int64
	CoverageFinds   int64
	CumulativeDelta float64
	SourceBreakdown map[string]*SourceStats
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
// Excludes aggregate totals (entries with square brackets like [mutation_total]).
func (p *ProviderStats) TopSources(n int) []string {
	if len(p.SourceBreakdown) == 0 {
		return nil
	}

	type sourceFind struct {
		name  string
		finds int64
	}
	sources := make([]sourceFind, 0, len(p.SourceBreakdown))
	for name, stats := range p.SourceBreakdown {
		// Skip aggregate totals (marked with square brackets)
		if len(name) > 0 && name[0] == '[' {
			continue
		}
		sources = append(sources, sourceFind{name, stats.CoverageFinds})
	}

	// Simple insertion sort
	for i := 1; i < len(sources); i++ {
		for j := i; j > 0 && sources[j].finds > sources[j-1].finds; j-- {
			sources[j], sources[j-1] = sources[j-1], sources[j]
		}
	}

	if n > len(sources) {
		n = len(sources)
	}
	result := make([]string, n)
	for i := 0; i < n; i++ {
		result[i] = fmt.Sprintf("%s(%d)", sources[i].name, sources[i].finds)
	}
	return result
}

// SourceStats tracks per-source statistics.
type SourceStats struct {
	Inputs        int64
	CoverageFinds int64
	TotalDelta    float64
}

// FindRate returns the find rate for this source.
func (s *SourceStats) FindRate() float64 {
	if s.Inputs == 0 {
		return 0
	}
	return float64(s.CoverageFinds) / float64(s.Inputs)
}
