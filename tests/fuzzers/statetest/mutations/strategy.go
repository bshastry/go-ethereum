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

// Package mutations provides mutation strategies for EVM state test fuzzing.
// Each strategy targets different aspects of the EVM to maximize coverage.
// This package is ported from goevmlab's fuzzing/mutations package.
package mutations

import (
	"errors"
	"math/rand"
)

// Common errors returned by mutation strategies.
var (
	// ErrNoMutableTarget is returned when a test has no suitable field to mutate.
	ErrNoMutableTarget = errors.New("no mutable target found in test")
	// ErrInvalidTest is returned when the test JSON format is invalid.
	ErrInvalidTest = errors.New("invalid test format")
)

// MutationStrategy defines the interface for state test mutation strategies.
// Strategies operate on raw JSON bytes to preserve all fields during mutation.
// Each implementation should be thread-safe when used with separate RNG instances.
type MutationStrategy interface {
	// Name returns the human-readable name of this strategy.
	Name() string

	// Description returns a brief description of what this strategy mutates.
	Description() string

	// Mutate performs mutation on raw JSON test data.
	// Returns the mutated JSON bytes and a description of the mutation performed.
	// Returns error if mutation is not possible for this test.
	Mutate(data []byte) ([]byte, string, error)

	// Weight returns the relative weight for this strategy in combined selection.
	// Higher weights mean more frequent selection.
	Weight() int
}

// CorpusProvider provides access to corpus data for splicing operations.
// This interface allows the splicing strategy to access other corpus entries
// without depending on the full corpus implementation.
type CorpusProvider interface {
	// GetRandomInput returns a random input from the corpus.
	GetRandomInput() ([]byte, error)
	// GetInputCount returns the number of inputs in the corpus.
	GetInputCount() int
}

// StrategyFactory creates mutation strategies by name.
// It maintains a registry of all available strategies.
type StrategyFactory struct {
	strategies map[string]func() MutationStrategy
	rng        *rand.Rand
}

// NewStrategyFactory creates a new factory with all registered strategies.
func NewStrategyFactory() *StrategyFactory {
	f := &StrategyFactory{
		strategies: make(map[string]func() MutationStrategy),
		rng:        rand.New(rand.NewSource(rand.Int63())),
	}

	// Register built-in strategies
	f.Register("bytecode", func() MutationStrategy { return NewBytecodeStrategy() })
	f.Register("opcode-smart", func() MutationStrategy { return NewOpcodeSmartStrategy() })
	f.Register("gas", func() MutationStrategy { return NewGasStrategy() })
	f.Register("value", func() MutationStrategy { return NewValueStrategy() })
	f.Register("calldata", func() MutationStrategy { return NewCalldataStrategy() })
	f.Register("storage", func() MutationStrategy { return NewStorageStrategy() })
	// Note: "env" strategy is disabled - mutating block environment (timestamp, number,
	// gasLimit, baseFee) causes false positives as it creates unrealistic test scenarios.

	// Phase 1: AFL-inspired strategies
	f.Register("arithmetic", func() MutationStrategy { return NewArithmeticStrategy() })
	f.Register("boundary", func() MutationStrategy { return NewBoundaryStrategy() })
	f.Register("dictionary", func() MutationStrategy { return NewDictionaryStrategy() })
	f.Register("bitflip", func() MutationStrategy { return NewBitFlipStrategy() })

	// Phase 2: Block operations and field mutations
	f.Register("blockops", func() MutationStrategy { return NewBlockOpsStrategy() })
	f.Register("txfields", func() MutationStrategy { return NewTransactionFieldStrategy() })
	f.Register("accountfields", func() MutationStrategy { return NewAccountFieldStrategy() })

	// Phase 3: Advanced AFL strategies
	f.Register("havoc", func() MutationStrategy { return NewHavocStrategy() })
	// Note: "splicing" is NOT registered by default - requires corpus access.
	// Use NewSplicingStrategy(corpus) directly when corpus is available.

	return f
}

// Register adds a strategy constructor to the factory.
func (f *StrategyFactory) Register(name string, constructor func() MutationStrategy) {
	f.strategies[name] = constructor
}

// Get returns a new instance of the named strategy.
func (f *StrategyFactory) Get(name string) (MutationStrategy, error) {
	constructor, ok := f.strategies[name]
	if !ok {
		return nil, errors.New("unknown strategy: " + name)
	}
	return constructor(), nil
}

// List returns all registered strategy names.
func (f *StrategyFactory) List() []string {
	names := make([]string, 0, len(f.strategies))
	for name := range f.strategies {
		names = append(names, name)
	}
	return names
}

// All returns new instances of all registered strategies.
func (f *StrategyFactory) All() []MutationStrategy {
	strategies := make([]MutationStrategy, 0, len(f.strategies))
	for _, constructor := range f.strategies {
		strategies = append(strategies, constructor())
	}
	return strategies
}

// CombinedStrategy wraps multiple strategies and selects one randomly
// based on weights for each mutation.
type CombinedStrategy struct {
	strategies   []MutationStrategy
	weights      []int
	totalWeight  int
	rng          *rand.Rand
	lastSelected string
}

// NewCombinedStrategy creates a strategy that randomly selects from multiple strategies.
func NewCombinedStrategy(strategies []MutationStrategy) *CombinedStrategy {
	totalWeight := 0
	weights := make([]int, len(strategies))
	for i, s := range strategies {
		weights[i] = s.Weight()
		totalWeight += weights[i]
	}

	return &CombinedStrategy{
		strategies:  strategies,
		weights:     weights,
		totalWeight: totalWeight,
		rng:         rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the combined strategy name.
func (c *CombinedStrategy) Name() string {
	return "combined"
}

// Description returns a description of the combined strategy.
func (c *CombinedStrategy) Description() string {
	return "Randomly selects from multiple mutation strategies"
}

// LastSelected returns the name of the last selected strategy.
func (c *CombinedStrategy) LastSelected() string {
	return c.lastSelected
}

// Mutate selects a random strategy based on weights and applies it.
// Returns (mutatedData, strategyName, error).
// Note: lastSelected may have race conditions with concurrent calls.
func (c *CombinedStrategy) Mutate(data []byte) ([]byte, string, error) {
	if len(c.strategies) == 0 {
		return nil, "", errors.New("no strategies configured")
	}

	// Select strategy by weight
	r := c.rng.Intn(c.totalWeight)
	cumulative := 0
	var selected MutationStrategy
	for i, s := range c.strategies {
		cumulative += c.weights[i]
		if r < cumulative {
			selected = s
			break
		}
	}

	if selected == nil {
		selected = c.strategies[0] // Fallback
	}

	// Store for LastSelected() (note: may have race with concurrent calls)
	c.lastSelected = selected.Name()

	// Apply the mutation
	result, _, err := selected.Mutate(data)
	return result, selected.Name(), err
}

// Weight returns weight of 1 (not used for combined).
func (c *CombinedStrategy) Weight() int {
	return 1
}

// StrategyResult holds the result of a mutation strategy evaluation.
type StrategyResult struct {
	StrategyName   string
	TestsProcessed int
	MutationsValid int
	NewLinesFound  int
	ErrorCount     int
	AvgMutationMs  float64
}

// StrategyEvaluation holds results for evaluating multiple strategies.
type StrategyEvaluation struct {
	BaselineCoveredLines int
	Results              []StrategyResult
}
