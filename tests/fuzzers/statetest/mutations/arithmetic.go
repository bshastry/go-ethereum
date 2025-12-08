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
	"math/big"
	"math/rand"
)

// ArithmeticStrategy applies AFL-style small arithmetic mutations (+-1-35)
// to numeric fields in state tests. This is effective for finding off-by-one
// bugs and boundary condition issues.
type ArithmeticStrategy struct {
	rng *rand.Rand
}

// ARITH_MAX is AFL's empirically-chosen maximum arithmetic delta.
const ARITH_MAX = 35

// maxUint256 is the maximum value for a 256-bit unsigned integer (2^256 - 1).
var maxUint256 = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

// NewArithmeticStrategy creates a new arithmetic mutation strategy.
func NewArithmeticStrategy() *ArithmeticStrategy {
	return &ArithmeticStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *ArithmeticStrategy) Name() string { return "arithmetic" }

// Description returns a brief description of this strategy.
func (s *ArithmeticStrategy) Description() string {
	return "AFL-style arithmetic mutations (+-1-35 on numeric fields)"
}

// Weight returns the relative weight for this strategy.
func (s *ArithmeticStrategy) Weight() int { return 12 } // High weight - effective for edge cases

// arithmeticTarget describes a target field for arithmetic mutation.
type arithmeticTarget struct {
	section  string   // "transaction", "env", or "pre"
	path     []string // Path within section
	isArray  bool     // Whether the field value is an array
	isBigInt bool     // Whether value is big.Int (hex string) vs uint64
	enabled  bool     // Whether this target is enabled for mutation
}

// arithmeticTargets defines targets for arithmetic mutation.
// Note: env fields and tx nonce are disabled - they cause false positives because
// mutating block environment or nonce creates unrealistic test scenarios.
var arithmeticTargets = []arithmeticTarget{
	// Transaction fields
	{section: "transaction", path: []string{"gasLimit"}, isArray: true, isBigInt: false, enabled: true},
	{section: "transaction", path: []string{"value"}, isArray: true, isBigInt: true, enabled: true},
	{section: "transaction", path: []string{"nonce"}, isArray: false, isBigInt: false, enabled: false}, // disabled: unrealistic
	{section: "transaction", path: []string{"gasPrice"}, isArray: false, isBigInt: true, enabled: true},
	{section: "transaction", path: []string{"maxFeePerGas"}, isArray: false, isBigInt: true, enabled: true},
	{section: "transaction", path: []string{"maxPriorityFeePerGas"}, isArray: false, isBigInt: true, enabled: true},

	// Environment fields - disabled: mutating block env causes false positives
	{section: "env", path: []string{"currentGasLimit"}, isArray: false, isBigInt: false, enabled: false},
	{section: "env", path: []string{"currentNumber"}, isArray: false, isBigInt: false, enabled: false},
	{section: "env", path: []string{"currentTimestamp"}, isArray: false, isBigInt: false, enabled: false},
	{section: "env", path: []string{"currentBaseFee"}, isArray: false, isBigInt: true, enabled: false},
}

// Mutate performs mutation on raw JSON test data.
func (s *ArithmeticStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Collect available targets (only enabled ones)
	var available []arithmeticTarget
	for _, target := range arithmeticTargets {
		if target.enabled {
			if _, ok := test[target.section]; ok {
				available = append(available, target)
			}
		}
	}

	// Also try to add account fields (balance, nonce in pre-state)
	if preData, ok := test["pre"]; ok {
		var pre map[string]json.RawMessage
		if json.Unmarshal(preData, &pre) == nil && len(pre) > 0 {
			// Add one balance and one nonce target for a random account
			available = append(available, arithmeticTarget{section: "pre", path: []string{"balance"}, isBigInt: true})
			available = append(available, arithmeticTarget{section: "pre", path: []string{"nonce"}, isBigInt: false})
		}
	}

	if len(available) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Select random target
	target := available[s.rng.Intn(len(available))]

	// Perform mutation
	var mutationDesc string
	var err error

	if target.section == "pre" {
		mutationDesc, err = s.mutatePreField(test, target)
	} else {
		mutationDesc, err = s.mutateSectionField(test, target)
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

	return result, mutationDesc, nil
}

func (s *ArithmeticStrategy) mutateSectionField(test map[string]json.RawMessage, target arithmeticTarget) (string, error) {
	sectionData, ok := test[target.section]
	if !ok {
		return "", ErrNoMutableTarget
	}

	var section map[string]json.RawMessage
	if err := json.Unmarshal(sectionData, &section); err != nil {
		return "", err
	}

	fieldName := target.path[0]
	fieldData, ok := section[fieldName]
	if !ok {
		return "", ErrNoMutableTarget
	}

	var newValue string
	var err error

	if target.isArray {
		newValue, err = s.mutateArrayField(fieldData, target.isBigInt)
		if err != nil {
			return "", err
		}
		newData, _ := json.Marshal([]string{newValue})
		section[fieldName] = newData
	} else {
		newValue, err = s.mutateSingleField(fieldData, target.isBigInt)
		if err != nil {
			return "", err
		}
		newData, _ := json.Marshal(newValue)
		section[fieldName] = newData
	}

	updatedSection, _ := json.Marshal(section)
	test[target.section] = updatedSection

	return fmt.Sprintf("%s.%s=%s", target.section, fieldName, newValue), nil
}

func (s *ArithmeticStrategy) mutatePreField(test map[string]json.RawMessage, target arithmeticTarget) (string, error) {
	preData, ok := test["pre"]
	if !ok {
		return "", ErrNoMutableTarget
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(preData, &pre); err != nil {
		return "", err
	}

	if len(pre) == 0 {
		return "", ErrNoMutableTarget
	}

	// Select random account
	accounts := make([]string, 0, len(pre))
	for addr := range pre {
		accounts = append(accounts, addr)
	}
	addr := accounts[s.rng.Intn(len(accounts))]

	var account map[string]json.RawMessage
	if err := json.Unmarshal(pre[addr], &account); err != nil {
		return "", err
	}

	fieldName := target.path[0]
	fieldData, ok := account[fieldName]
	if !ok {
		return "", ErrNoMutableTarget
	}

	newValue, err := s.mutateSingleField(fieldData, target.isBigInt)
	if err != nil {
		return "", err
	}

	newData, _ := json.Marshal(newValue)
	account[fieldName] = newData

	updatedAccount, _ := json.Marshal(account)
	pre[addr] = updatedAccount

	updatedPre, _ := json.Marshal(pre)
	test["pre"] = updatedPre

	return fmt.Sprintf("pre[%s].%s=%s", addr, fieldName, newValue), nil
}

func (s *ArithmeticStrategy) mutateArrayField(data json.RawMessage, isBigInt bool) (string, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil {
		// Try as single value
		return s.mutateSingleField(data, isBigInt)
	}

	if len(arr) == 0 {
		return "", ErrNoMutableTarget
	}

	// Mutate first element
	return s.mutateSingleField(arr[0], isBigInt)
}

func (s *ArithmeticStrategy) mutateSingleField(data json.RawMessage, isBigInt bool) (string, error) {
	var hexStr string
	if err := json.Unmarshal(data, &hexStr); err != nil {
		return "", err
	}

	if isBigInt {
		return s.mutateBigIntHex(hexStr), nil
	}
	return s.mutateUint64Hex(hexStr), nil
}

func (s *ArithmeticStrategy) mutateUint64Hex(hexStr string) string {
	val := hexToUint64(hexStr)
	delta := uint64(1 + s.rng.Intn(ARITH_MAX))

	if s.rng.Intn(2) == 0 {
		val += delta
	} else if val >= delta {
		val -= delta
	} else {
		val = 0
	}

	return fmt.Sprintf("0x%x", val)
}

func (s *ArithmeticStrategy) mutateBigIntHex(hexStr string) string {
	val := hexToBigInt(hexStr)
	if val == nil {
		val = big.NewInt(0)
	}

	delta := big.NewInt(int64(1 + s.rng.Intn(ARITH_MAX)))

	if s.rng.Intn(2) == 0 {
		val = new(big.Int).Add(val, delta)
		// Clamp to 256-bit maximum to avoid overflow
		if val.Cmp(maxUint256) > 0 {
			val = new(big.Int).Set(maxUint256)
		}
	} else {
		val = new(big.Int).Sub(val, delta)
		if val.Sign() < 0 {
			val = big.NewInt(0)
		}
	}

	return "0x" + val.Text(16)
}

// hexToBigInt parses a hex string to big.Int.
func hexToBigInt(s string) *big.Int {
	s = trimHexPrefix(s)
	if s == "" {
		return big.NewInt(0)
	}
	val, ok := new(big.Int).SetString(s, 16)
	if !ok {
		return big.NewInt(0)
	}
	return val
}

// trimHexPrefix removes 0x prefix from a hex string.
func trimHexPrefix(s string) string {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:]
	}
	return s
}
