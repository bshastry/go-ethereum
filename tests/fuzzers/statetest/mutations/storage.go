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

// StorageStrategy mutates pre-state storage to test state management.
type StorageStrategy struct {
	rng *rand.Rand
}

// NewStorageStrategy creates a new storage mutation strategy.
func NewStorageStrategy() *StorageStrategy {
	return &StorageStrategy{rng: rand.New(rand.NewSource(rand.Int63()))}
}

// Name returns the strategy name.
func (s *StorageStrategy) Name() string { return "storage" }

// Description returns a brief description of this strategy.
func (s *StorageStrategy) Description() string { return "Storage mutations (keys, values, warm/cold)" }

// Weight returns the relative weight for this strategy.
func (s *StorageStrategy) Weight() int { return 7 }

// truncateSlot safely truncates a slot address for display.
func truncateSlot(slot string) string {
	if len(slot) <= 10 {
		return slot
	}
	return slot[:10]
}

// interestingSlots contains interesting storage slots.
var interestingSlots = []string{
	"0x" + strings.Repeat("00", 32),        // Slot 0
	"0x" + strings.Repeat("00", 31) + "01", // Slot 1
	"0x" + strings.Repeat("ff", 32),        // Max slot
	"0x" + strings.Repeat("ff", 31) + "fe", // Max - 1
	"0x" + strings.Repeat("00", 31) + "ff", // Slot 255
}

// interestingStorageValues contains interesting storage values.
var interestingStorageValues = []string{
	"0x" + strings.Repeat("00", 32),        // Zero (delete)
	"0x" + strings.Repeat("00", 31) + "01", // Non-zero minimal
	"0x" + strings.Repeat("ff", 32),        // Max value
	"0x" + strings.Repeat("de", 32),        // Pattern
	"0x80" + strings.Repeat("00", 31),      // Sign bit
}

// Mutate performs mutation on raw JSON test data.
func (s *StorageStrategy) Mutate(data []byte) ([]byte, string, error) {
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

	// Find accounts with storage or code
	var candidates []string
	for addr, accountData := range pre {
		var account map[string]json.RawMessage
		if err := json.Unmarshal(accountData, &account); err != nil {
			continue
		}
		// Prefer accounts with existing storage or code
		if _, hasStorage := account["storage"]; hasStorage {
			candidates = append(candidates, addr)
		} else if _, hasCode := account["code"]; hasCode {
			candidates = append(candidates, addr)
		}
	}

	if len(candidates) == 0 {
		// Use any account
		for addr := range pre {
			candidates = append(candidates, addr)
			break
		}
	}

	if len(candidates) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	targetAddr := candidates[s.rng.Intn(len(candidates))]

	var account map[string]json.RawMessage
	_ = json.Unmarshal(pre[targetAddr], &account) // Best effort

	// Get or create storage
	var storage map[string]string
	if storageData, ok := account["storage"]; ok {
		_ = json.Unmarshal(storageData, &storage) // Best effort
	}
	if storage == nil {
		storage = make(map[string]string)
	}

	strategy := s.rng.Intn(100)
	var mutation string

	switch {
	case strategy < 40:
		// Add new storage slot
		slot := interestingSlots[s.rng.Intn(len(interestingSlots))]
		value := interestingStorageValues[s.rng.Intn(len(interestingStorageValues))]
		storage[slot] = value
		mutation = fmt.Sprintf("add slot %s", truncateSlot(slot))

	case strategy < 70 && len(storage) > 0:
		// Modify existing slot
		var existingSlot string
		for k := range storage {
			existingSlot = k
			break
		}
		newValue := interestingStorageValues[s.rng.Intn(len(interestingStorageValues))]
		storage[existingSlot] = newValue
		mutation = fmt.Sprintf("mod slot %s", truncateSlot(existingSlot))

	case strategy < 85 && len(storage) > 0:
		// Delete slot (set to zero)
		var existingSlot string
		for k := range storage {
			existingSlot = k
			break
		}
		storage[existingSlot] = "0x" + strings.Repeat("00", 32)
		mutation = fmt.Sprintf("zero slot %s", truncateSlot(existingSlot))

	default:
		// Add random slot
		randSlot := make([]byte, 32)
		s.rng.Read(randSlot)
		slotHex := "0x" + fmt.Sprintf("%064x", randSlot)
		value := interestingStorageValues[s.rng.Intn(len(interestingStorageValues))]
		storage[slotHex] = value
		mutation = "random slot"
	}

	storageData, _ := json.Marshal(storage)
	account["storage"] = storageData

	updatedAccount, _ := json.Marshal(account)
	pre[targetAddr] = updatedAccount

	updatedPre, _ := json.Marshal(pre)
	test["pre"] = updatedPre

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", err
	}

	return result, mutation, nil
}
