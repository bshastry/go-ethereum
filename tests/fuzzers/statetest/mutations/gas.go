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
	"strconv"
	"strings"
)

// GasStrategy mutates transaction gas limits to test gas metering edge cases.
type GasStrategy struct {
	rng *rand.Rand
}

// NewGasStrategy creates a new gas mutation strategy.
func NewGasStrategy() *GasStrategy {
	return &GasStrategy{rng: rand.New(rand.NewSource(rand.Int63()))}
}

// Name returns the strategy name.
func (s *GasStrategy) Name() string { return "gas" }

// Description returns a brief description of this strategy.
func (s *GasStrategy) Description() string { return "Gas limit mutations (boundaries, just-enough)" }

// Weight returns the relative weight for this strategy.
func (s *GasStrategy) Weight() int { return 8 }

// interestingGasValues contains interesting gas values for fuzzing.
var interestingGasValues = []uint64{
	0,
	1,
	21000,          // Base tx cost
	21001,          // Just above base
	20999,          // Just below base
	53000,          // CREATE cost region
	32000,          // CALL stipend region
	2300,           // Call stipend
	2600,           // COLD_ACCOUNT_ACCESS (EIP-2929)
	100,            // WARM_STORAGE_READ
	20000,          // SSTORE_SET
	5000,           // SSTORE_RESET
	100000,         // Common test value
	1000000,        // Higher gas
	10000000,       // 10M gas
	30000000,       // Block gas limit region
	0xFFFFFFFF,     // Max uint32
	0xFFFFFFFFFFFF, // Large value
}

// Mutate performs mutation on raw JSON test data.
func (s *GasStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Get current gas limit
	gasData, ok := tx["gasLimit"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var gasValues []json.RawMessage
	if err := json.Unmarshal(gasData, &gasValues); err != nil {
		// Try single value
		var gasHex string
		if err := json.Unmarshal(gasData, &gasHex); err != nil {
			return nil, "", err
		}
		gasValues = []json.RawMessage{gasData}
	}

	if len(gasValues) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Mutate the first gas value
	var newGas uint64
	strategy := s.rng.Intn(100)

	switch {
	case strategy < 40:
		// Use interesting value
		newGas = interestingGasValues[s.rng.Intn(len(interestingGasValues))]
	case strategy < 70:
		// Parse current and adjust
		var gasHex string
		_ = json.Unmarshal(gasValues[0], &gasHex) // Best effort
		currentGas := hexToUint64(gasHex)
		delta := uint64(s.rng.Intn(10000))
		if s.rng.Intn(2) == 0 {
			newGas = currentGas + delta
		} else if currentGas > delta {
			newGas = currentGas - delta
		} else {
			newGas = 0
		}
	default:
		// Random gas
		newGas = uint64(s.rng.Int63n(30000000))
	}

	newGasHex := fmt.Sprintf("0x%x", newGas)
	newGasData, _ := json.Marshal([]string{newGasHex})
	tx["gasLimit"] = newGasData

	updatedTx, _ := json.Marshal(tx)
	test["transaction"] = updatedTx

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", err
	}

	return result, fmt.Sprintf("gas=%s", newGasHex), nil
}

// hexToUint64 parses a hex string to uint64.
func hexToUint64(s string) uint64 {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0
	}
	val, _ := strconv.ParseUint(s, 16, 64)
	return val
}
