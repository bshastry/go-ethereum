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

package mutations

// RawMutator is an adapter that makes MutationStrategy compatible with
// the corpus fuzzer's expected interface.
//
// Integration with corpus-fuzzer:
//
//	mutator := mutations.NewRawMutator("combined") // or specific strategy
//	mutatedData, strategyName, err := mutator.MutateRawJSON(data)
type RawMutator struct {
	strategy MutationStrategy
	factory  *StrategyFactory
}

// NewRawMutator creates a mutator compatible with the corpus fuzzer.
// strategyName can be:
//   - A specific strategy: "bytecode", "opcode-smart", "gas", etc.
//   - "combined" for weighted random selection from all strategies
func NewRawMutator(strategyName string) *RawMutator {
	factory := NewStrategyFactory()

	var strategy MutationStrategy
	if strategyName == "combined" {
		strategy = NewCombinedStrategy(factory.All())
	} else {
		s, err := factory.Get(strategyName)
		if err != nil {
			// Fall back to bytecode if unknown strategy
			s, _ = factory.Get("bytecode")
		}
		strategy = s
	}

	return &RawMutator{
		strategy: strategy,
		factory:  factory,
	}
}

// NewRawMutatorFromStrategy creates a mutator from an existing strategy.
// This allows creating a mutator with a pre-configured combined strategy
// that includes corpus-aware strategies like splicing.
func NewRawMutatorFromStrategy(strategy MutationStrategy, factory *StrategyFactory) *RawMutator {
	return &RawMutator{
		strategy: strategy,
		factory:  factory,
	}
}

// NewRawMutatorWithStrategies creates a combined mutator with specific strategies.
// This allows fine-tuning which strategies are used based on evaluation results.
func NewRawMutatorWithStrategies(strategyNames []string) *RawMutator {
	factory := NewStrategyFactory()

	var strategies []MutationStrategy
	for _, name := range strategyNames {
		if s, err := factory.Get(name); err == nil {
			strategies = append(strategies, s)
		}
	}

	if len(strategies) == 0 {
		// Fall back to bytecode
		s, _ := factory.Get("bytecode")
		strategies = append(strategies, s)
	}

	return &RawMutator{
		strategy: NewCombinedStrategy(strategies),
		factory:  factory,
	}
}

// NewRawMutatorWithCorpus creates a mutator that includes corpus-aware strategies.
// This is useful for enabling splicing mutations when a corpus is available.
func NewRawMutatorWithCorpus(strategyName string, corpus CorpusProvider) *RawMutator {
	factory := NewStrategyFactory()

	// Register splicing strategy with corpus
	if corpus != nil && corpus.GetInputCount() >= 2 {
		factory.Register("splicing", func() MutationStrategy {
			return NewSplicingStrategy(corpus)
		})
	}

	var strategy MutationStrategy
	if strategyName == "combined" {
		strategy = NewCombinedStrategy(factory.All())
	} else {
		s, err := factory.Get(strategyName)
		if err != nil {
			s, _ = factory.Get("bytecode")
		}
		strategy = s
	}

	return &RawMutator{
		strategy: strategy,
		factory:  factory,
	}
}

// Name returns the mutator name for logging.
func (m *RawMutator) Name() string {
	return m.strategy.Name()
}

// MutateRawJSON applies the mutation strategy to raw JSON test data.
// This method signature matches what corpus.go expects.
// Returns (mutatedData, mutationDescription, error).
func (m *RawMutator) MutateRawJSON(data []byte) ([]byte, string, error) {
	return m.strategy.Mutate(data)
}

// Strategy returns the underlying strategy (for logging/debugging).
func (m *RawMutator) Strategy() MutationStrategy {
	return m.strategy
}

// SetStrategy changes the active strategy.
func (m *RawMutator) SetStrategy(name string) error {
	s, err := m.factory.Get(name)
	if err != nil {
		return err
	}
	m.strategy = s
	return nil
}

// AvailableStrategies returns all registered strategy names.
func (m *RawMutator) AvailableStrategies() []string {
	return m.factory.List()
}

// LastSelectedStrategy returns the name of the last strategy used (for combined).
func (m *RawMutator) LastSelectedStrategy() string {
	if combined, ok := m.strategy.(*CombinedStrategy); ok {
		return combined.LastSelected()
	}
	return m.strategy.Name()
}
