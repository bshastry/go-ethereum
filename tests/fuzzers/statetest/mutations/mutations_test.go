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
	"testing"
)

// sampleStateTest is a minimal state test for mutation testing.
var sampleStateTest = `{
  "test": {
    "env": {
      "currentCoinbase": "0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba",
      "currentGasLimit": "0x1c9c380",
      "currentNumber": "0x1",
      "currentTimestamp": "0x1",
      "currentBaseFee": "0x7"
    },
    "pre": {
      "0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b": {
        "balance": "0xde0b6b3a7640000",
        "code": "0x60016001600160016001600155600035600052",
        "nonce": "0x0",
        "storage": {
          "0x0000000000000000000000000000000000000000000000000000000000000001": "0x0000000000000000000000000000000000000000000000000000000000000002"
        }
      },
      "0xb94f5374fce5edbc8e2a8697c15331677e6ebf0b": {
        "balance": "0xde0b6b3a7640000",
        "code": "0x",
        "nonce": "0x0",
        "storage": {}
      }
    },
    "transaction": {
      "data": ["0x"],
      "gasLimit": ["0x5208"],
      "gasPrice": "0x1",
      "nonce": "0x0",
      "secretKey": "0x45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8",
      "sender": "0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b",
      "to": "0xb94f5374fce5edbc8e2a8697c15331677e6ebf0b",
      "value": ["0x1"]
    }
  }
}`

func TestStrategyFactory(t *testing.T) {
	factory := NewStrategyFactory()

	// Verify all expected strategies are registered
	expectedStrategies := []string{
		"bytecode", "opcode-smart", "gas", "value", "calldata", "storage",
		"arithmetic", "boundary", "dictionary", "bitflip",
		"blockops", "txfields", "accountfields", "havoc",
	}

	strategies := factory.List()
	strategyMap := make(map[string]bool)
	for _, s := range strategies {
		strategyMap[s] = true
	}

	for _, expected := range expectedStrategies {
		if !strategyMap[expected] {
			t.Errorf("expected strategy %q not found in factory", expected)
		}
	}
}

func TestAllStrategiesCanMutate(t *testing.T) {
	factory := NewStrategyFactory()
	testData := []byte(sampleStateTest)

	for _, name := range factory.List() {
		t.Run(name, func(t *testing.T) {
			strategy, err := factory.Get(name)
			if err != nil {
				t.Fatalf("failed to get strategy %q: %v", name, err)
			}

			// Try mutation (may fail for some strategies depending on test data)
			result, desc, err := strategy.Mutate(testData)
			if err != nil {
				// Some strategies may not be able to mutate this specific test
				t.Logf("strategy %q returned error (may be expected): %v", name, err)
				return
			}

			if len(result) == 0 {
				t.Errorf("strategy %q returned empty result", name)
			}

			// Verify result is valid JSON
			var parsed map[string]interface{}
			if err := json.Unmarshal(result, &parsed); err != nil {
				t.Errorf("strategy %q returned invalid JSON: %v", name, err)
			}

			t.Logf("strategy %q mutated successfully: %s", name, desc)
		})
	}
}

func TestCombinedStrategy(t *testing.T) {
	factory := NewStrategyFactory()
	combined := NewCombinedStrategy(factory.All())

	testData := []byte(sampleStateTest)

	// Run multiple mutations to test weighted selection
	successCount := 0
	for i := 0; i < 20; i++ {
		result, strategyName, err := combined.Mutate(testData)
		if err != nil {
			continue
		}

		if len(result) == 0 {
			t.Error("combined strategy returned empty result")
		}

		if strategyName == "" {
			t.Error("combined strategy returned empty strategy name")
		}

		successCount++
	}

	if successCount == 0 {
		t.Error("combined strategy failed all mutation attempts")
	}

	t.Logf("combined strategy succeeded %d/20 times", successCount)
}

func TestRawMutator(t *testing.T) {
	mutator := NewRawMutator("combined")

	testData := []byte(sampleStateTest)

	result, desc, err := mutator.MutateRawJSON(testData)
	if err != nil {
		t.Fatalf("MutateRawJSON failed: %v", err)
	}

	if len(result) == 0 {
		t.Error("MutateRawJSON returned empty result")
	}

	// Verify result is valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Errorf("MutateRawJSON returned invalid JSON: %v", err)
	}

	t.Logf("mutation description: %s", desc)
}

func TestRawMutatorWithStrategies(t *testing.T) {
	mutator := NewRawMutatorWithStrategies([]string{"bytecode", "gas", "value"})

	testData := []byte(sampleStateTest)

	successCount := 0
	for i := 0; i < 10; i++ {
		result, _, err := mutator.MutateRawJSON(testData)
		if err == nil && len(result) > 0 {
			successCount++
		}
	}

	if successCount == 0 {
		t.Error("mutator with specific strategies failed all attempts")
	}
}

func TestHavocStrategy(t *testing.T) {
	havoc := NewHavocStrategy()

	// Verify havoc doesn't include itself (prevent recursion)
	for _, s := range havoc.GetStrategies() {
		if s.Name() == "havoc" {
			t.Error("havoc strategy incorrectly includes itself")
		}
	}

	testData := []byte(sampleStateTest)

	// Havoc should successfully mutate
	result, desc, err := havoc.Mutate(testData)
	if err != nil {
		t.Fatalf("havoc mutation failed: %v", err)
	}

	if len(result) == 0 {
		t.Error("havoc returned empty result")
	}

	t.Logf("havoc result: %s", desc)
}

func TestBytecodeHelpers(t *testing.T) {
	// Test hexToBytes
	bytes, err := hexToBytes("0x1234")
	if err != nil {
		t.Errorf("hexToBytes failed: %v", err)
	}
	if len(bytes) != 2 || bytes[0] != 0x12 || bytes[1] != 0x34 {
		t.Errorf("hexToBytes returned unexpected result: %x", bytes)
	}

	// Test bytesToHex
	hex := bytesToHex([]byte{0xab, 0xcd})
	if hex != "0xabcd" {
		t.Errorf("bytesToHex returned unexpected result: %s", hex)
	}

	// Test empty cases
	emptyBytes, _ := hexToBytes("0x")
	if len(emptyBytes) != 0 {
		t.Errorf("hexToBytes(0x) should return empty slice")
	}

	emptyHex := bytesToHex(nil)
	if emptyHex != "0x" {
		t.Errorf("bytesToHex(nil) should return 0x")
	}
}

func TestFindMutablePositions(t *testing.T) {
	// Test simple bytecode
	code := []byte{0x60, 0x01, 0x60, 0x02, 0x01} // PUSH1 1 PUSH1 2 ADD
	positions := findMutablePositions(code)

	// Should find positions 0, 2, 4 (opcodes, not PUSH operands)
	expected := []int{0, 2, 4}
	if len(positions) != len(expected) {
		t.Errorf("expected %d positions, got %d", len(expected), len(positions))
	}

	for i, pos := range positions {
		if pos != expected[i] {
			t.Errorf("position %d: expected %d, got %d", i, expected[i], pos)
		}
	}
}

// MockCorpusProvider implements CorpusProvider for testing splicing.
type MockCorpusProvider struct {
	inputs [][]byte
}

func (m *MockCorpusProvider) GetRandomInput() ([]byte, error) {
	if len(m.inputs) == 0 {
		return nil, ErrNoMutableTarget
	}
	return m.inputs[0], nil
}

func (m *MockCorpusProvider) GetInputCount() int {
	return len(m.inputs)
}

func TestSplicingStrategy(t *testing.T) {
	// Create mock corpus
	corpus := &MockCorpusProvider{
		inputs: [][]byte{
			[]byte(sampleStateTest),
			[]byte(sampleStateTest), // Same for simplicity
		},
	}

	splicing := NewSplicingStrategy(corpus)

	// Splicing needs different bytecode to work effectively
	// For this test, we just verify it doesn't panic
	testData := []byte(sampleStateTest)
	_, _, err := splicing.Mutate(testData)
	// Error is expected since the two test inputs have identical bytecode
	if err == nil {
		t.Log("splicing succeeded (bytecode differed)")
	} else {
		t.Logf("splicing returned expected error: %v", err)
	}
}

func TestArithmeticHelpers(t *testing.T) {
	// Test hexToUint64
	val := hexToUint64("0xff")
	if val != 255 {
		t.Errorf("hexToUint64(0xff) = %d, want 255", val)
	}

	// Test hexToBigInt
	bigVal := hexToBigInt("0x100")
	if bigVal.Int64() != 256 {
		t.Errorf("hexToBigInt(0x100) = %s, want 256", bigVal.String())
	}

	// Test trimHexPrefix
	trimmed := trimHexPrefix("0xabc")
	if trimmed != "abc" {
		t.Errorf("trimHexPrefix(0xabc) = %s, want abc", trimmed)
	}

	trimmed = trimHexPrefix("abc")
	if trimmed != "abc" {
		t.Errorf("trimHexPrefix(abc) = %s, want abc", trimmed)
	}
}
