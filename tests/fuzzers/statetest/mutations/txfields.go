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

// TransactionFieldStrategy mutates transaction fields that aren't covered
// by existing strategies (nonce, gasPrice, to, maxFeePerGas, etc.).
type TransactionFieldStrategy struct {
	rng *rand.Rand
}

// NewTransactionFieldStrategy creates a new transaction field mutation strategy.
func NewTransactionFieldStrategy() *TransactionFieldStrategy {
	return &TransactionFieldStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *TransactionFieldStrategy) Name() string { return "txfields" }

// Description returns a brief description of this strategy.
func (s *TransactionFieldStrategy) Description() string {
	return "Mutate transaction fields (nonce, gasPrice, to, maxFeePerGas)"
}

// Weight returns the relative weight for this strategy.
func (s *TransactionFieldStrategy) Weight() int { return 9 }

// interestingToAddresses are interesting 'to' addresses for mutation.
var interestingToAddresses = []string{
	"",                                           // Empty (CREATE)
	"0x0000000000000000000000000000000000000000", // Zero address
	"0x0000000000000000000000000000000000000001", // ECRECOVER
	"0x0000000000000000000000000000000000000002", // SHA256
	"0x0000000000000000000000000000000000000003", // RIPEMD160
	"0x0000000000000000000000000000000000000004", // IDENTITY
	"0x0000000000000000000000000000000000000005", // MODEXP
	"0x0000000000000000000000000000000000000006", // BN254_ADD
	"0x0000000000000000000000000000000000000007", // BN254_MUL
	"0x0000000000000000000000000000000000000008", // BN254_PAIRING
	"0x0000000000000000000000000000000000000009", // BLAKE2F
	"0x000000000000000000000000000000000000000a", // KZG_POINT_EVAL
	"0xffffffffffffffffffffffffffffffffffffffff", // Max address
}

// interestingNonces are interesting nonce values for mutation.
var interestingNonces = []string{
	"0x0", "0x1", "0xff", "0xffff", "0xffffffff", "0xffffffffffffffff",
}

// interestingGasPrices are interesting gas price values for mutation.
var interestingGasPrices = []string{
	"0x0", "0x1", "0x3b9aca00",   // 0, 1, 1 gwei
	"0x2540be400",                // 10 gwei
	"0x174876e800",               // 100 gwei
	"0xffffffffffffffff",         // Max uint64
}

// txFieldTarget describes a target field for transaction mutation.
type txFieldTarget struct {
	name   string
	mutate func(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error)
}

// Mutate performs mutation on raw JSON test data.
func (s *TransactionFieldStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Define available targets based on what exists in the transaction
	var targets []txFieldTarget

	if _, ok := tx["nonce"]; ok {
		targets = append(targets, txFieldTarget{name: "nonce", mutate: mutateNonce})
	}
	if _, ok := tx["gasPrice"]; ok {
		targets = append(targets, txFieldTarget{name: "gasPrice", mutate: mutateGasPrice})
	}
	if _, ok := tx["to"]; ok {
		targets = append(targets, txFieldTarget{name: "to", mutate: mutateTo})
	}
	if _, ok := tx["maxFeePerGas"]; ok {
		targets = append(targets, txFieldTarget{name: "maxFeePerGas", mutate: mutateMaxFeePerGas})
	}
	if _, ok := tx["maxPriorityFeePerGas"]; ok {
		targets = append(targets, txFieldTarget{name: "maxPriorityFeePerGas", mutate: mutateMaxPriorityFeePerGas})
	}
	if _, ok := tx["maxFeePerBlobGas"]; ok {
		targets = append(targets, txFieldTarget{name: "maxFeePerBlobGas", mutate: mutateMaxFeePerBlobGas})
	}

	if len(targets) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	// Select random target
	target := targets[s.rng.Intn(len(targets))]

	// Perform mutation
	desc, err := target.mutate(s, tx)
	if err != nil {
		return nil, "", err
	}

	// Rebuild JSON
	updatedTx, _ := json.Marshal(tx)
	test["transaction"] = updatedTx

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return result, fmt.Sprintf("tx.%s", desc), nil
}

func mutateNonce(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	newValue := interestingNonces[s.rng.Intn(len(interestingNonces))]
	newData, _ := json.Marshal(newValue)
	tx["nonce"] = newData
	return "nonce=" + newValue, nil
}

func mutateGasPrice(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	newValue := interestingGasPrices[s.rng.Intn(len(interestingGasPrices))]
	newData, _ := json.Marshal(newValue)
	tx["gasPrice"] = newData
	return "gasPrice=" + newValue, nil
}

func mutateTo(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	newValue := interestingToAddresses[s.rng.Intn(len(interestingToAddresses))]
	if newValue == "" {
		// For CREATE transactions, use empty string or remove the field
		tx["to"] = json.RawMessage(`""`)
		return "to=(create)", nil
	}
	newData, _ := json.Marshal(newValue)
	tx["to"] = newData
	return "to=" + newValue, nil
}

func mutateMaxFeePerGas(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	newValue := interestingGasPrices[s.rng.Intn(len(interestingGasPrices))]
	newData, _ := json.Marshal(newValue)
	tx["maxFeePerGas"] = newData
	return "maxFeePerGas=" + newValue, nil
}

func mutateMaxPriorityFeePerGas(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	newValue := interestingGasPrices[s.rng.Intn(len(interestingGasPrices))]
	newData, _ := json.Marshal(newValue)
	tx["maxPriorityFeePerGas"] = newData
	return "maxPriorityFeePerGas=" + newValue, nil
}

func mutateMaxFeePerBlobGas(s *TransactionFieldStrategy, tx map[string]json.RawMessage) (string, error) {
	// Blob gas prices can be similar to regular gas prices
	newValue := interestingGasPrices[s.rng.Intn(len(interestingGasPrices))]
	newData, _ := json.Marshal(newValue)
	tx["maxFeePerBlobGas"] = newData
	return "maxFeePerBlobGas=" + newValue, nil
}
