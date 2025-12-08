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

import (
	"fmt"
	"math/rand"
)

// HavocStrategy applies multiple stacked mutations (AFL havoc stage).
// This is AFL's most effective mutation stage for finding deep bugs.
// It stacks 2-128 random mutations per test case, dramatically increasing
// mutation diversity.
type HavocStrategy struct {
	strategies []MutationStrategy
	rng        *rand.Rand
}

// NewHavocStrategy creates a havoc strategy with all base strategies.
// Note: HavocStrategy is not included in its own base strategies to prevent recursion.
func NewHavocStrategy() *HavocStrategy {
	// Include all base strategies except HavocStrategy itself
	baseStrategies := []MutationStrategy{
		NewBytecodeStrategy(),
		NewOpcodeSmartStrategy(),
		NewGasStrategy(),
		NewValueStrategy(),
		NewCalldataStrategy(),
		NewStorageStrategy(),
		NewArithmeticStrategy(),
		NewBoundaryStrategy(),
		NewDictionaryStrategy(),
		NewBitFlipStrategy(),
		NewBlockOpsStrategy(),
		NewTransactionFieldStrategy(),
		NewAccountFieldStrategy(),
	}
	return &HavocStrategy{
		strategies: baseStrategies,
		rng:        rand.New(rand.NewSource(rand.Int63())),
	}
}

// NewHavocStrategyWithStrategies creates a havoc strategy with custom base strategies.
// This is useful for testing or when you want to limit the mutation pool.
func NewHavocStrategyWithStrategies(strategies []MutationStrategy) *HavocStrategy {
	return &HavocStrategy{
		strategies: strategies,
		rng:        rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (h *HavocStrategy) Name() string { return "havoc" }

// Description returns a brief description of this strategy.
func (h *HavocStrategy) Description() string {
	return "AFL havoc stage: stack 2-128 random mutations"
}

// Weight returns the relative weight for this strategy.
// Lower weight (5) since havoc is expensive and should be used less frequently.
func (h *HavocStrategy) Weight() int { return 5 }

// Mutate applies multiple stacked mutations to the test data.
// Stack count follows AFL's formula: 2^(1 + random(0-6)) = 2, 4, 8, 16, 32, 64, or 128.
func (h *HavocStrategy) Mutate(data []byte) ([]byte, string, error) {
	if len(h.strategies) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Stack count: 2^(1 + random(0-6)) = 2, 4, 8, 16, 32, 64, or 128
	stackPow := 1 + h.rng.Intn(7)
	stackCount := 1 << stackPow

	result := make([]byte, len(data))
	copy(result, data)

	successCount := 0
	appliedStrategies := make([]string, 0, stackCount)

	for i := 0; i < stackCount; i++ {
		// Select random strategy (uniform distribution - AFL style)
		strategy := h.strategies[h.rng.Intn(len(h.strategies))]

		mutated, _, err := strategy.Mutate(result)
		if err == nil && mutated != nil {
			result = mutated
			successCount++
			appliedStrategies = append(appliedStrategies, strategy.Name())
		}
		// Continue even on errors - some mutations may not apply to certain test cases
	}

	if successCount == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Return a summary of applied mutations
	return result, fmt.Sprintf("havoc[%d/%d]", successCount, stackCount), nil
}

// GetStrategies returns the list of base strategies used by this havoc strategy.
// This is useful for testing to verify no infinite recursion.
func (h *HavocStrategy) GetStrategies() []MutationStrategy {
	return h.strategies
}
