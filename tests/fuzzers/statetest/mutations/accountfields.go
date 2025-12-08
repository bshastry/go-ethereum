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
)

// AccountFieldStrategy mutates account balance and nonce in pre-state.
type AccountFieldStrategy struct {
	rng *rand.Rand
}

// NewAccountFieldStrategy creates a new account field mutation strategy.
func NewAccountFieldStrategy() *AccountFieldStrategy {
	return &AccountFieldStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *AccountFieldStrategy) Name() string { return "accountfields" }

// Description returns a brief description of this strategy.
func (s *AccountFieldStrategy) Description() string {
	return "Mutate account balance and nonce in pre-state"
}

// Weight returns the relative weight for this strategy.
func (s *AccountFieldStrategy) Weight() int { return 6 }

// interestingBalances are interesting balance values for mutation.
// Note: U256_MAX excluded - causes false positives (REVM aborts on balance overflow, geth wraps).
var interestingBalances = []string{
	"0x0",                 // Zero
	"0x1",                 // 1 wei
	"0xde0b6b3a7640000",   // 1 ether
	"0x6f05b59d3b20000",   // 0.5 ether
	"0x1bc16d674ec80000",  // 2 ether
	"0x56bc75e2d63100000", // 100 ether
}

// interestingAccountNonces are interesting nonce values for account mutation.
var interestingAccountNonces = []string{
	"0x0", "0x1", "0xff", "0x100", "0xffff", "0xffffffff",
}

// Mutate performs mutation on raw JSON test data.
func (s *AccountFieldStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	preData, ok := test["pre"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(preData, &pre); err != nil {
		return nil, "", err
	}

	if len(pre) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Select random account
	accounts := make([]string, 0, len(pre))
	for addr := range pre {
		accounts = append(accounts, addr)
	}
	addr := accounts[s.rng.Intn(len(accounts))]

	var account map[string]json.RawMessage
	if err := json.Unmarshal(pre[addr], &account); err != nil {
		return nil, "", err
	}

	// 50% chance: mutate balance, 50% chance: mutate nonce
	var desc string
	if s.rng.Intn(2) == 0 {
		// Mutate balance
		newValue := interestingBalances[s.rng.Intn(len(interestingBalances))]
		newData, _ := json.Marshal(newValue)
		account["balance"] = newData
		desc = fmt.Sprintf("pre[%s].balance=%s", addr, newValue)
	} else {
		// Mutate nonce
		newValue := interestingAccountNonces[s.rng.Intn(len(interestingAccountNonces))]
		newData, _ := json.Marshal(newValue)
		account["nonce"] = newData
		desc = fmt.Sprintf("pre[%s].nonce=%s", addr, newValue)
	}

	// Rebuild JSON
	updatedAccount, _ := json.Marshal(account)
	pre[addr] = updatedAccount

	updatedPre, _ := json.Marshal(pre)
	test["pre"] = updatedPre

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return result, desc, nil
}
