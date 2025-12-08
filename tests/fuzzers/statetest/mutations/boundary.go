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

// BoundaryStrategy replaces numeric values with AFL-inspired boundary/interesting
// values that are likely to trigger edge case bugs.
type BoundaryStrategy struct {
	rng *rand.Rand
}

// NewBoundaryStrategy creates a new boundary value mutation strategy.
func NewBoundaryStrategy() *BoundaryStrategy {
	return &BoundaryStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *BoundaryStrategy) Name() string { return "boundary" }

// Description returns a brief description of this strategy.
func (s *BoundaryStrategy) Description() string {
	return "Replace values with AFL-inspired boundary/interesting values"
}

// Weight returns the relative weight for this strategy.
func (s *BoundaryStrategy) Weight() int { return 10 }

// EVM-specific interesting gas values (uint64).
var interestingGasBoundaries = []string{
	"0x0",                // Zero
	"0x1",                // Minimum
	"0x51bf",             // 21000-1 (just below intrinsic)
	"0x5208",             // 21000 (intrinsic gas cost)
	"0x5209",             // 21000+1 (just above intrinsic)
	"0x8fc",              // 2300 (CALL stipend)
	"0xa28",              // 2600 (COLD_ACCOUNT_ACCESS EIP-2929)
	"0x64",               // 100 (WARM_STORAGE_READ)
	"0x4e20",             // 20000 (SSTORE_SET)
	"0x1388",             // 5000 (SSTORE_RESET)
	"0x7d00",             // 32000 (CREATE cost)
	"0xcf08",             // 53000 (CREATE2 cost region)
	"0x1c9c380",          // 30000000 (block gas limit)
	"0xffffffff",         // Max uint32
	"0xffffffffffff",     // Large uint48
	"0xffffffffffffffff", // Max uint64
}

// EVM-specific interesting value amounts (big.Int as hex).
// Note: Max uint256 is excluded as it causes false positives in realistic scenarios.
var interestingValueBoundaries = []string{
	"0x0",                                  // Zero
	"0x1",                                  // 1 wei
	"0xde0b6b3a7640000",                    // 1 ether
	"0x8ac7230489e80000",                   // 10 ether
	"0x152d02c7e14af6800000",               // 100,000 ether
	"0x7fffffffffffffff",                   // Max int64
	"0xffffffffffffffff",                   // Max uint64
	"0x" + strings.Repeat("ff", 16),        // Max uint128
	"0x80" + strings.Repeat("00", 31),      // Sign bit set
	"0x" + strings.Repeat("00", 31) + "01", // 1 with leading zeros
}

// EVM-specific interesting timestamps.
var interestingTimestampBoundaries = []string{
	"0x0",        // Genesis
	"0x1",        // 1 second
	"0x7fffffff", // Max int32 (Y2K38)
	"0xffffffff", // Max uint32
	"0x5f5e100",  // 100M seconds (~3 years)
	"0x65a1bc00", // ~2024 timestamp
	"0x77359400", // Year 2033 approx
}

// EVM-specific interesting block numbers.
var interestingBlockNumberBoundaries = []string{
	"0x0",        // Genesis
	"0x1",        // Block 1
	"0x1fff",     // 8191 (EIP-2935 ring buffer size - 1)
	"0x2000",     // 8192 (EIP-2935 ring buffer size)
	"0x2001",     // 8193
	"0xf4240",    // 1,000,000
	"0x989680",   // 10,000,000
	"0xffffffff", // Max uint32
}

// EVM-specific interesting base fees.
var interestingBaseFeeBoundaries = []string{
	"0x1",          // Minimum
	"0x7",          // Very low
	"0x3b9aca00",   // 1 gwei
	"0x77359400",   // 2 gwei
	"0xe8d4a51000", // 1000 gwei
}

// boundaryTarget defines a target for boundary mutation.
type boundaryTarget struct {
	section     string
	field       string
	isArray     bool
	boundaries  []string
	description string
	enabled     bool // Whether this target is enabled for mutation
}

// boundaryTargets defines targets for boundary mutation.
// Note: env fields are disabled - they cause false positives because mutating block
// environment creates unrealistic test scenarios.
var boundaryTargets = []boundaryTarget{
	{section: "transaction", field: "gasLimit", isArray: true, boundaries: interestingGasBoundaries, description: "gas", enabled: true},
	{section: "transaction", field: "value", isArray: true, boundaries: interestingValueBoundaries, description: "value", enabled: true},
	{section: "transaction", field: "gasPrice", isArray: false, boundaries: interestingBaseFeeBoundaries, description: "gasPrice", enabled: true},
	{section: "transaction", field: "maxFeePerGas", isArray: false, boundaries: interestingBaseFeeBoundaries, description: "maxFeePerGas", enabled: true},
	{section: "transaction", field: "maxPriorityFeePerGas", isArray: false, boundaries: interestingBaseFeeBoundaries, description: "maxPriorityFeePerGas", enabled: true},
	// Environment fields - disabled: mutating block env causes false positives
	{section: "env", field: "currentGasLimit", isArray: false, boundaries: interestingGasBoundaries, description: "blockGasLimit", enabled: false},
	{section: "env", field: "currentTimestamp", isArray: false, boundaries: interestingTimestampBoundaries, description: "timestamp", enabled: false},
	{section: "env", field: "currentNumber", isArray: false, boundaries: interestingBlockNumberBoundaries, description: "blockNumber", enabled: false},
	{section: "env", field: "currentBaseFee", isArray: false, boundaries: interestingBaseFeeBoundaries, description: "baseFee", enabled: false},
}

// Mutate performs mutation on raw JSON test data.
func (s *BoundaryStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Find available targets (only enabled ones)
	var available []boundaryTarget
	for _, target := range boundaryTargets {
		if !target.enabled {
			continue
		}
		if sectionData, ok := test[target.section]; ok {
			var section map[string]json.RawMessage
			if json.Unmarshal(sectionData, &section) == nil {
				if _, ok := section[target.field]; ok {
					available = append(available, target)
				}
			}
		}
	}

	// Also add account balance boundaries
	if preData, ok := test["pre"]; ok {
		var pre map[string]json.RawMessage
		if json.Unmarshal(preData, &pre) == nil && len(pre) > 0 {
			available = append(available, boundaryTarget{
				section:     "pre",
				field:       "balance",
				boundaries:  interestingValueBoundaries,
				description: "accountBalance",
			})
		}
	}

	if len(available) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Select random target
	target := available[s.rng.Intn(len(available))]

	// Select random boundary value
	newValue := target.boundaries[s.rng.Intn(len(target.boundaries))]

	// Apply mutation
	var err error
	if target.section == "pre" {
		err = s.mutatePreField(test, target.field, newValue)
	} else {
		err = s.mutateSectionField(test, target, newValue)
	}

	if err != nil {
		return nil, "", err
	}

	// Rebuild JSON
	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return result, fmt.Sprintf("%s=%s", target.description, newValue), nil
}

func (s *BoundaryStrategy) mutateSectionField(test map[string]json.RawMessage, target boundaryTarget, newValue string) error {
	sectionData := test[target.section]

	var section map[string]json.RawMessage
	if err := json.Unmarshal(sectionData, &section); err != nil {
		return err
	}

	if target.isArray {
		newData, _ := json.Marshal([]string{newValue})
		section[target.field] = newData
	} else {
		newData, _ := json.Marshal(newValue)
		section[target.field] = newData
	}

	updatedSection, _ := json.Marshal(section)
	test[target.section] = updatedSection

	return nil
}

func (s *BoundaryStrategy) mutatePreField(test map[string]json.RawMessage, field, newValue string) error {
	preData, ok := test["pre"]
	if !ok {
		return ErrNoMutableTarget
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(preData, &pre); err != nil {
		return err
	}

	if len(pre) == 0 {
		return ErrNoMutableTarget
	}

	// Select random account
	accounts := make([]string, 0, len(pre))
	for addr := range pre {
		accounts = append(accounts, addr)
	}
	addr := accounts[s.rng.Intn(len(accounts))]

	var account map[string]json.RawMessage
	if err := json.Unmarshal(pre[addr], &account); err != nil {
		return err
	}

	newData, _ := json.Marshal(newValue)
	account[field] = newData

	updatedAccount, _ := json.Marshal(account)
	pre[addr] = updatedAccount

	updatedPre, _ := json.Marshal(pre)
	test["pre"] = updatedPre

	return nil
}
