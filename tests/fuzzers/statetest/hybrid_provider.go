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

package statetest

import (
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest/generators"
)

// HybridConfig configures the hybrid mutation+generation provider.
type HybridConfig struct {
	// MutationRatio is the probability of using mutation (0.0 to 1.0).
	// 0.7 means 70% mutation, 30% generation.
	MutationRatio float64

	// Fork is the target fork for generation.
	Fork string

	// Adaptive enables dynamic ratio adjustment based on coverage finds.
	Adaptive bool
}

// HybridProvider combines mutation and generation strategies.
// It uses a configurable ratio to mix inputs from both sources,
// with optional adaptive adjustment based on which source finds more coverage.
type HybridProvider struct {
	mutation   *MutationProvider
	generation *generators.GeneratorProvider
	corpus     *CoverageCorpus
	config     HybridConfig

	mu  sync.Mutex
	rng *rand.Rand

	// Per-source tracking for adaptive ratio
	mutInputs  int64
	mutFinds   int64
	mutDelta   float64
	genInputs  int64
	genFinds   int64
	genDelta   float64

	// Combined stats
	stats *statsTracker
}

// NewHybridProvider creates a provider that mixes mutation and generation.
func NewHybridProvider(
	mutation *MutationProvider,
	generation *generators.GeneratorProvider,
	corpus *CoverageCorpus,
	config HybridConfig,
) *HybridProvider {
	// Validate and clamp ratio
	if config.MutationRatio < 0 {
		config.MutationRatio = 0
	} else if config.MutationRatio > 1 {
		config.MutationRatio = 1
	}

	return &HybridProvider{
		mutation:   mutation,
		generation: generation,
		corpus:     corpus,
		config:     config,
		rng:        rand.New(rand.NewSource(time.Now().UnixNano())),
		stats:      newStatsTracker(),
	}
}

// Name returns the provider name.
func (p *HybridProvider) Name() string {
	return "hybrid"
}

// Next returns the next input from either mutation or generation.
func (p *HybridProvider) Next() ([]byte, string, error) {
	p.mu.Lock()
	useMutation := p.rng.Float64() < p.config.MutationRatio
	p.mu.Unlock()

	if useMutation {
		return p.mutationNext()
	}
	return p.generationNext()
}

func (p *HybridProvider) mutationNext() ([]byte, string, error) {
	atomic.AddInt64(&p.mutInputs, 1)

	data, source, err := p.mutation.Next()
	if err != nil {
		// Fall back to generation if mutation fails (e.g., empty corpus)
		return p.generationNext()
	}

	p.stats.recordInput(source)
	return data, source, nil
}

func (p *HybridProvider) generationNext() ([]byte, string, error) {
	atomic.AddInt64(&p.genInputs, 1)

	data, source, err := p.generation.Next()
	if err != nil {
		return nil, "", err
	}

	p.stats.recordInput(source)
	return data, source, nil
}

// Feedback records coverage feedback and routes it appropriately.
func (p *HybridProvider) Feedback(data []byte, source string, coverageDelta float64) {
	p.stats.recordFeedback(source, coverageDelta)

	// Route to appropriate provider
	if strings.HasPrefix(source, "mutation:") {
		p.mutation.Feedback(data, source, coverageDelta)
		if coverageDelta > 0 {
			atomic.AddInt64(&p.mutFinds, 1)
			p.mu.Lock()
			p.mutDelta += coverageDelta
			p.mu.Unlock()
		}
	} else if strings.HasPrefix(source, "generation:") {
		p.generation.Feedback(data, source, coverageDelta)
		if coverageDelta > 0 {
			atomic.AddInt64(&p.genFinds, 1)
			p.mu.Lock()
			p.genDelta += coverageDelta
			p.mu.Unlock()

			// KEY: Also add generation finds to corpus for mutation
			// This is the cross-feeding that combines both approaches
			p.corpus.AddHighPriority(&PriorityInput{
				Data:           data,
				Priority:       int(coverageDelta * 1000000),
				CoverageDelta:  coverageDelta,
				DiscoveredAt:   time.Now(),
				ParentStrategy: source,
			})
		}
	}

	// Adaptive ratio adjustment
	if p.config.Adaptive && coverageDelta > 0 {
		p.adjustRatio()
	}
}

// adjustRatio dynamically adjusts the mutation ratio based on recent performance.
func (p *HybridProvider) adjustRatio() {
	mutI := atomic.LoadInt64(&p.mutInputs)
	genI := atomic.LoadInt64(&p.genInputs)

	// Need enough data for meaningful adjustment
	if mutI < 1000 || genI < 1000 {
		return
	}

	mutF := atomic.LoadInt64(&p.mutFinds)
	genF := atomic.LoadInt64(&p.genFinds)

	// Calculate find rates
	mutRate := float64(mutF) / float64(mutI)
	genRate := float64(genF) / float64(genI)

	// Adjust ratio toward the more productive approach
	// Use exponential moving average for stability
	const alpha = 0.1
	if mutRate+genRate > 0 {
		targetRatio := mutRate / (mutRate + genRate)

		p.mu.Lock()
		p.config.MutationRatio = p.config.MutationRatio*(1-alpha) + targetRatio*alpha

		// Clamp to reasonable bounds - don't abandon either approach
		if p.config.MutationRatio < 0.2 {
			p.config.MutationRatio = 0.2
		} else if p.config.MutationRatio > 0.9 {
			p.config.MutationRatio = 0.9
		}
		p.mu.Unlock()
	}
}

// Stats returns combined statistics from both providers.
func (p *HybridProvider) Stats() ProviderStats {
	combined := p.stats.stats(p.Name())

	// Add metadata about the current ratio and source performance
	mutI := atomic.LoadInt64(&p.mutInputs)
	genI := atomic.LoadInt64(&p.genInputs)
	mutF := atomic.LoadInt64(&p.mutFinds)
	genF := atomic.LoadInt64(&p.genFinds)

	// Ensure mutation and generation categories are in breakdown
	if combined.SourceBreakdown == nil {
		combined.SourceBreakdown = make(map[string]*SourceStats)
	}

	// Add summary stats for mutation category
	if mutI > 0 {
		combined.SourceBreakdown["[mutation_total]"] = &SourceStats{
			Inputs:        mutI,
			CoverageFinds: mutF,
			TotalDelta:    p.mutDelta,
		}
	}

	// Add summary stats for generation category
	if genI > 0 {
		combined.SourceBreakdown["[generation_total]"] = &SourceStats{
			Inputs:        genI,
			CoverageFinds: genF,
			TotalDelta:    p.genDelta,
		}
	}

	return combined
}

// CurrentRatio returns the current mutation ratio.
func (p *HybridProvider) CurrentRatio() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.config.MutationRatio
}

// IsAdaptive returns whether adaptive ratio adjustment is enabled.
func (p *HybridProvider) IsAdaptive() bool {
	return p.config.Adaptive
}

// MutationStats returns statistics for the mutation source.
func (p *HybridProvider) MutationStats() (inputs, finds int64, delta float64) {
	p.mu.Lock()
	delta = p.mutDelta
	p.mu.Unlock()
	return atomic.LoadInt64(&p.mutInputs), atomic.LoadInt64(&p.mutFinds), delta
}

// GenerationStats returns statistics for the generation source.
func (p *HybridProvider) GenerationStats() (inputs, finds int64, delta float64) {
	p.mu.Lock()
	delta = p.genDelta
	p.mu.Unlock()
	return atomic.LoadInt64(&p.genInputs), atomic.LoadInt64(&p.genFinds), delta
}
