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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

// CalldataStrategy mutates transaction calldata to test input handling.
type CalldataStrategy struct {
	rng *rand.Rand
}

// NewCalldataStrategy creates a new calldata mutation strategy.
func NewCalldataStrategy() *CalldataStrategy {
	return &CalldataStrategy{rng: rand.New(rand.NewSource(rand.Int63()))}
}

// Name returns the strategy name.
func (s *CalldataStrategy) Name() string { return "calldata" }

// Description returns a brief description of this strategy.
func (s *CalldataStrategy) Description() string {
	return "Calldata mutations (length, content, patterns)"
}

// Weight returns the relative weight for this strategy.
func (s *CalldataStrategy) Weight() int { return 8 }

// interestingCalldata contains interesting calldata patterns.
var interestingCalldata = []string{
	"0x",                                   // Empty
	"0x00",                                 // Single zero
	"0xff",                                 // Single 0xff
	"0x" + strings.Repeat("00", 32),        // 32 zeros
	"0x" + strings.Repeat("ff", 32),        // 32 0xff
	"0x" + strings.Repeat("00", 64),        // 64 zeros (common ABI)
	"0x" + strings.Repeat("00", 31) + "01", // Minimal non-zero last byte
	"0xa9059cbb",                           // transfer(address,uint256) selector
	"0x23b872dd",                           // transferFrom selector
	"0x095ea7b3",                           // approve selector
	"0x70a08231",                           // balanceOf selector
}

// Mutate performs mutation on raw JSON test data.
func (s *CalldataStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Get current data field
	calldataField, ok := tx["data"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var calldataArray []json.RawMessage
	if err := json.Unmarshal(calldataField, &calldataArray); err != nil {
		calldataArray = []json.RawMessage{calldataField}
	}

	if len(calldataArray) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Get current calldata
	var currentHex string
	_ = json.Unmarshal(calldataArray[0], &currentHex) // Best effort
	currentBytes, _ := hexToBytes(currentHex)

	var newCalldata string
	strategy := s.rng.Intn(100)

	switch {
	case strategy < 30:
		// Use interesting pattern
		newCalldata = interestingCalldata[s.rng.Intn(len(interestingCalldata))]

	case strategy < 50:
		// Mutate existing calldata
		if len(currentBytes) > 0 {
			mutated := make([]byte, len(currentBytes))
			copy(mutated, currentBytes)
			pos := s.rng.Intn(len(mutated))
			mutated[pos] = byte(s.rng.Intn(256))
			newCalldata = "0x" + hex.EncodeToString(mutated)
		} else {
			newCalldata = interestingCalldata[s.rng.Intn(len(interestingCalldata))]
		}

	case strategy < 70:
		// Extend calldata
		extraLen := s.rng.Intn(64) + 1
		extra := make([]byte, extraLen)
		s.rng.Read(extra)
		newBytes := append(currentBytes, extra...)
		newCalldata = "0x" + hex.EncodeToString(newBytes)

	case strategy < 85:
		// Truncate calldata
		if len(currentBytes) > 1 {
			newLen := s.rng.Intn(len(currentBytes))
			newCalldata = "0x" + hex.EncodeToString(currentBytes[:newLen])
		} else {
			newCalldata = "0x"
		}

	default:
		// Random calldata
		randLen := s.rng.Intn(256)
		randBytes := make([]byte, randLen)
		s.rng.Read(randBytes)
		newCalldata = "0x" + hex.EncodeToString(randBytes)
	}

	newCalldataData, _ := json.Marshal([]string{newCalldata})
	tx["data"] = newCalldataData

	updatedTx, _ := json.Marshal(tx)
	test["transaction"] = updatedTx

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", err
	}

	return result, fmt.Sprintf("calldata_len=%d", len(newCalldata)/2-1), nil
}
