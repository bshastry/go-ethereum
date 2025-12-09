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
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest/generators"
	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest/mutations"
)

// createGeneratorProvider creates a generator-only provider.
// This implementation is used when built with -tags=generators.
func createGeneratorProvider(t *testing.T, corpus *CoverageCorpus) InputProvider {
	fork := GetEnvOrDefault("FUZZ_FORK", "Prague")
	genNames := os.Getenv("FUZZ_GENERATORS")

	registry := generators.NewRegistry()
	if registry == nil {
		return nil
	}

	var provider *generators.GeneratorProvider
	if genNames != "" {
		// Use specific generators
		names := strings.Split(genNames, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		provider = generators.NewGeneratorProviderWithNames(registry, fork, names)
	} else {
		// Use all generators for the fork
		provider = generators.NewGeneratorProvider(registry, fork)
	}

	t.Logf("Generator provider: fork=%s, generators=%d (%s)",
		fork, provider.GeneratorCount(), strings.Join(provider.GeneratorNames(), ", "))

	// Wrap in adapter to implement statetest.InputProvider
	return &generatorProviderAdapter{
		provider: provider,
		corpus:   corpus,
	}
}

// createHybridProvider creates a hybrid mutation+generation provider.
// This implementation is used when built with -tags=generators.
func createHybridProvider(t *testing.T, corpus *CoverageCorpus, strategy string) InputProvider {
	fork := GetEnvOrDefault("FUZZ_FORK", "Prague")
	ratio := ParseFloatOrDefault("FUZZ_MUTATION_RATIO", 0.7)
	adaptive := ParseBoolOrDefault("FUZZ_ADAPTIVE_RATIO", false)
	genNames := os.Getenv("FUZZ_GENERATORS")

	registry := generators.NewRegistry()
	if registry == nil {
		return nil
	}

	// Create generator provider
	var genProvider *generators.GeneratorProvider
	if genNames != "" {
		names := strings.Split(genNames, ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		genProvider = generators.NewGeneratorProviderWithNames(registry, fork, names)
	} else {
		genProvider = generators.NewGeneratorProvider(registry, fork)
	}

	// Create mutation provider
	mutator := mutations.NewRawMutatorWithCorpus(strategy, corpus)
	mutProvider := NewMutationProvider(corpus, mutator)

	t.Logf("Hybrid provider: ratio=%.2f, adaptive=%v, fork=%s, generators=%d",
		ratio, adaptive, fork, genProvider.GeneratorCount())

	return NewHybridProvider(mutProvider, genProvider, corpus, HybridConfig{
		MutationRatio: ratio,
		Adaptive:      adaptive,
		Fork:          fork,
	})
}

// generatorProviderAdapter adapts generators.GeneratorProvider to statetest.InputProvider.
type generatorProviderAdapter struct {
	provider *generators.GeneratorProvider
	corpus   *CoverageCorpus
}

func (a *generatorProviderAdapter) Name() string {
	return a.provider.Name()
}

func (a *generatorProviderAdapter) Next() ([]byte, string, error) {
	return a.provider.Next()
}

func (a *generatorProviderAdapter) Feedback(data []byte, source string, coverageDelta float64) {
	a.provider.Feedback(data, source, coverageDelta)

	// Also add to corpus for potential future mutation (cross-feeding)
	if coverageDelta > 0 && a.corpus != nil {
		a.corpus.AddHighPriority(&PriorityInput{
			Data:           data,
			Priority:       int(coverageDelta * 1000000),
			CoverageDelta:  coverageDelta,
			ParentStrategy: source,
		})
	}
}

func (a *generatorProviderAdapter) Stats() ProviderStats {
	gs := a.provider.Stats()
	// Convert from generators.ProviderStats to statetest.ProviderStats
	breakdown := make(map[string]*SourceStats, len(gs.SourceBreakdown))
	for k, v := range gs.SourceBreakdown {
		breakdown[k] = &SourceStats{
			Inputs:        v.Inputs,
			CoverageFinds: v.CoverageFinds,
			TotalDelta:    v.TotalDelta,
		}
	}
	return ProviderStats{
		ProviderName:    gs.ProviderName,
		TotalInputs:     gs.TotalInputs,
		CoverageFinds:   gs.CoverageFinds,
		CumulativeDelta: gs.CumulativeDelta,
		SourceBreakdown: breakdown,
	}
}
