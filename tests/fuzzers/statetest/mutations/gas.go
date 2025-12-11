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

// EIP-7825 Gas Limit Constants
const (
	// MaxTxGasEIP7825 is the EIP-7825 transaction gas limit cap (Osaka/Fusaka).
	// Transactions with gasLimit > 2^24 are invalid post-Osaka.
	MaxTxGasEIP7825 uint64 = 1 << 24 // 16,777,216

	// MaxTxGasPreOsaka is the max gas for pre-Osaka forks (block gas limit region).
	MaxTxGasPreOsaka uint64 = 30_000_000
)

// GasStrategy mutates transaction gas limits to test gas metering edge cases.
type GasStrategy struct {
	rng      *rand.Rand
	forkName string
}

// NewGasStrategy creates a new gas mutation strategy with default (pre-Osaka) behavior.
func NewGasStrategy() *GasStrategy {
	return &GasStrategy{
		rng:      rand.New(rand.NewSource(rand.Int63())),
		forkName: "",
	}
}

// NewGasStrategyForFork creates a new gas mutation strategy configured for a specific fork.
func NewGasStrategyForFork(fork string) *GasStrategy {
	return &GasStrategy{
		rng:      rand.New(rand.NewSource(rand.Int63())),
		forkName: fork,
	}
}

// Name returns the strategy name.
func (s *GasStrategy) Name() string { return "gas" }

// Description returns a brief description of this strategy.
func (s *GasStrategy) Description() string { return "Gas limit mutations (boundaries, just-enough)" }

// Weight returns the relative weight for this strategy.
func (s *GasStrategy) Weight() int { return 8 }

// interestingGasValuesCommon contains gas values valid for all forks (values <= 10M).
var interestingGasValuesCommon = []uint64{
	0,
	1,
	21000,    // Base tx cost
	21001,    // Just above base
	20999,    // Just below base
	53000,    // CREATE cost region
	32000,    // CALL stipend region
	2300,     // Call stipend
	2600,     // COLD_ACCOUNT_ACCESS (EIP-2929)
	100,      // WARM_STORAGE_READ
	20000,    // SSTORE_SET
	5000,     // SSTORE_RESET
	100000,   // Common test value
	1000000,  // Higher gas
	10000000, // 10M gas
}

// interestingGasValuesEIP7825Boundary contains EIP-7825 boundary values (2^24 region).
var interestingGasValuesEIP7825Boundary = []uint64{
	MaxTxGasEIP7825 - 1, // 16,777,215 - Just under cap (valid)
	MaxTxGasEIP7825,     // 16,777,216 - At cap (valid)
	MaxTxGasEIP7825 + 1, // 16,777,217 - Just over cap (invalid post-Osaka)
}

// interestingGasValuesPreOsaka contains large gas values only valid pre-Osaka.
var interestingGasValuesPreOsaka = []uint64{
	30000000,       // Block gas limit region
	0xFFFFFFFF,     // Max uint32
	0xFFFFFFFFFFFF, // Large value
}

// IsPostOsaka returns true if this strategy is configured for Osaka or later forks.
func (s *GasStrategy) IsPostOsaka() bool {
	switch s.forkName {
	case "Osaka", "Fusaka":
		return true
	default:
		return false
	}
}

// GetInterestingGasValues returns the interesting gas values for the configured fork.
// For post-Osaka forks, it includes EIP-7825 boundary values and excludes
// pre-Osaka-only large values. For pre-Osaka forks, it includes all values.
func (s *GasStrategy) GetInterestingGasValues() []uint64 {
	if s.IsPostOsaka() {
		// Post-Osaka: common values + EIP-7825 boundary values
		result := make([]uint64, 0, len(interestingGasValuesCommon)+len(interestingGasValuesEIP7825Boundary))
		result = append(result, interestingGasValuesCommon...)
		result = append(result, interestingGasValuesEIP7825Boundary...)
		return result
	}
	// Pre-Osaka: common values + pre-Osaka large values
	result := make([]uint64, 0, len(interestingGasValuesCommon)+len(interestingGasValuesPreOsaka))
	result = append(result, interestingGasValuesCommon...)
	result = append(result, interestingGasValuesPreOsaka...)
	return result
}

// GetMaxRandomGas returns the maximum random gas value for the configured fork.
// For post-Osaka forks, this is 2^24 (EIP-7825 cap).
// For pre-Osaka forks, this is 30M (block gas limit region).
func (s *GasStrategy) GetMaxRandomGas() uint64 {
	if s.IsPostOsaka() {
		return MaxTxGasEIP7825
	}
	return MaxTxGasPreOsaka
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

	// Get fork-aware interesting values and max random gas
	interestingValues := s.GetInterestingGasValues()
	maxRandomGas := s.GetMaxRandomGas()

	switch {
	case strategy < 40:
		// Use interesting value (fork-aware)
		newGas = interestingValues[s.rng.Intn(len(interestingValues))]
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
		// Random gas (fork-aware max)
		newGas = uint64(s.rng.Int63n(int64(maxRandomGas)))
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
