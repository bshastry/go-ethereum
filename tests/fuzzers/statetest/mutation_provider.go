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
	"encoding/json"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest/mutations"
)

// MutationProvider wraps the existing corpus + mutation flow as an InputProvider.
// This is the baseline provider that implements the original fuzzer behavior.
type MutationProvider struct {
	corpus  *CoverageCorpus
	mutator *mutations.RawMutator
	stats   *statsTracker
}

// NewMutationProvider creates a provider that uses corpus-based mutation.
// This wraps the existing fuzzer flow into the InputProvider interface.
func NewMutationProvider(corpus *CoverageCorpus, mutator *mutations.RawMutator) *MutationProvider {
	return &MutationProvider{
		corpus:  corpus,
		mutator: mutator,
		stats:   newStatsTracker(),
	}
}

// Name returns the provider name.
func (p *MutationProvider) Name() string {
	return "mutation"
}

// Next returns the next mutated input from the corpus.
// Implements InputProvider.Next().
func (p *MutationProvider) Next() ([]byte, string, error) {
	// Get next input from corpus (high priority first, then round-robin seeds)
	input := p.corpus.Pop()
	if input == nil {
		return nil, "", ErrProviderExhausted
	}

	// Check if this is a cross-VM entry that needs verification instead of mutation
	crossVMMeta := p.extractCrossVMMetadata(input)
	if crossVMMeta != nil && crossVMMeta.GeneratedBy != "" &&
		!strings.EqualFold(crossVMMeta.GeneratedBy, "geth") {
		// Return as-is for cross-VM verification
		p.stats.recordInput("crossvm:" + crossVMMeta.GeneratedBy)
		return input, "crossvm:" + crossVMMeta.GeneratedBy, nil
	}

	// Strip stale metadata before mutation
	cleaned, err := StripCrossVMMetadata(input)
	if err != nil {
		cleaned = input
	}

	// Mutate the input
	mutated, strategyName, err := p.mutator.MutateRawJSON(cleaned)
	if err != nil {
		// Fall back to original input
		p.stats.recordInput("mutation:original")
		return cleaned, "mutation:original", nil
	}

	source := "mutation:" + strategyName
	p.stats.recordInput(source)
	return mutated, source, nil
}

// Feedback records coverage feedback and adds successful inputs to the corpus.
// Implements InputProvider.Feedback().
func (p *MutationProvider) Feedback(data []byte, source string, coverageDelta float64) {
	p.stats.recordFeedback(source, coverageDelta)

	if coverageDelta > 0 {
		// Add to high priority queue for further mutation
		p.corpus.AddHighPriority(&PriorityInput{
			Data:           data,
			Priority:       int(coverageDelta * 1000000), // Scale for int comparison
			CoverageDelta:  coverageDelta,
			DiscoveredAt:   time.Now(),
			ParentStrategy: source,
		})
	}
}

// Stats returns current provider statistics.
// Implements InputProvider.Stats().
func (p *MutationProvider) Stats() ProviderStats {
	return p.stats.stats(p.Name())
}

// Corpus returns the underlying corpus for direct access if needed.
func (p *MutationProvider) Corpus() *CoverageCorpus {
	return p.corpus
}

// Mutator returns the underlying mutator for direct access if needed.
func (p *MutationProvider) Mutator() *mutations.RawMutator {
	return p.mutator
}

// extractCrossVMMetadata extracts cross-VM metadata from a test JSON if present.
func (p *MutationProvider) extractCrossVMMetadata(testJSON []byte) *CrossVMMetadata {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &raw); err != nil {
		return nil
	}
	return extractCrossVMMetadataFromRaw(raw)
}

// MutationProviderOption is a functional option for configuring MutationProvider.
type MutationProviderOption func(*MutationProvider)

// NewMutationProviderWithOptions creates a provider with custom options.
func NewMutationProviderWithOptions(
	corpus *CoverageCorpus,
	mutator *mutations.RawMutator,
	opts ...MutationProviderOption,
) *MutationProvider {
	p := NewMutationProvider(corpus, mutator)
	for _, opt := range opts {
		opt(p)
	}
	return p
}
