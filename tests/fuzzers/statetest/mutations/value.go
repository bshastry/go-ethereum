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
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

// ValueStrategy mutates transaction values to test value transfer edge cases.
type ValueStrategy struct {
	rng *rand.Rand
}

// NewValueStrategy creates a new value mutation strategy.
func NewValueStrategy() *ValueStrategy {
	return &ValueStrategy{rng: rand.New(rand.NewSource(rand.Int63()))}
}

// Name returns the strategy name.
func (s *ValueStrategy) Name() string { return "value" }

// Description returns a brief description of this strategy.
func (s *ValueStrategy) Description() string {
	return "Transaction value mutations (zero, max, boundaries)"
}

// Weight returns the relative weight for this strategy.
func (s *ValueStrategy) Weight() int { return 6 }

// interestingValues contains interesting value strings (hex) for fuzzing.
// Note: Very large values (>uint64) are excluded as they cause false positives
// when used as transaction values - real-world txs don't have such huge values.
var interestingValues = []string{
	"0x0",                                  // Zero
	"0x1",                                  // One wei
	"0xde0b6b3a7640000",                    // 1 ether
	"0x8ac7230489e80000",                   // 10 ether
	"0xffffffffffffffff",                   // Max uint64
	"0x80" + strings.Repeat("00", 31),      // Sign bit set
	"0x" + strings.Repeat("00", 31) + "ff", // Small with leading zeros
}

// Mutate performs mutation on raw JSON test data.
func (s *ValueStrategy) Mutate(data []byte) ([]byte, string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(raw) == 0 {
		return nil, "", ErrInvalidTest
	}

	var testName string
	var testData json.RawMessage
	for name, d := range raw {
		testName = name
		testData = d
		break
	}

	var test map[string]json.RawMessage
	if err := json.Unmarshal(testData, &test); err != nil {
		return nil, "", err
	}

	txData, ok := test["transaction"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var tx map[string]json.RawMessage
	if err := json.Unmarshal(txData, &tx); err != nil {
		return nil, "", err
	}

	// Get current value
	valueData, ok := tx["value"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var valueArray []json.RawMessage
	if err := json.Unmarshal(valueData, &valueArray); err != nil {
		// Try single value
		valueArray = []json.RawMessage{valueData}
	}

	if len(valueArray) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Select new value
	var newValue string
	strategy := s.rng.Intn(100)

	switch {
	case strategy < 50:
		// Use interesting value
		newValue = interestingValues[s.rng.Intn(len(interestingValues))]
	case strategy < 80:
		// Random bytes as value
		numBytes := s.rng.Intn(32) + 1
		bytes := make([]byte, numBytes)
		s.rng.Read(bytes)
		newValue = "0x" + fmt.Sprintf("%x", bytes)
	default:
		// Zero value
		newValue = "0x0"
	}

	newValueData, _ := json.Marshal([]string{newValue})
	tx["value"] = newValueData

	updatedTx, _ := json.Marshal(tx)
	test["transaction"] = updatedTx

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", err
	}

	return result, fmt.Sprintf("value=%s", newValue), nil
}
