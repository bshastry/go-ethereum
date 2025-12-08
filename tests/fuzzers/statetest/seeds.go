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

package statetest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/tests"
)

// LoadSeeds loads JSON state test files from a directory
func LoadSeeds(dir string) ([][]byte, error) {
	var seeds [][]byte

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".json") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			// Skip unreadable files but continue walking
			return nil
		}

		// Validate it's a valid state test JSON
		if !isValidStateTestJSON(data) {
			return nil
		}

		seeds = append(seeds, data)
		return nil
	})

	return seeds, err
}

// LoadSeedsFromFiles loads seeds from specific file paths
func LoadSeedsFromFiles(paths []string) ([][]byte, error) {
	var seeds [][]byte

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable files
		}

		if !isValidStateTestJSON(data) {
			continue
		}

		seeds = append(seeds, data)
	}

	return seeds, nil
}

// IsValidStateTestJSON checks if the data looks like a valid state test (exported)
func IsValidStateTestJSON(data []byte) bool {
	return isValidStateTestJSON(data)
}

// isValidStateTestJSON checks if the data looks like a valid state test
func isValidStateTestJSON(data []byte) bool {
	// Quick check: must be valid JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return false
	}

	// Must have at least one test case
	if len(raw) == 0 {
		return false
	}

	// Check if any entry looks like a state test (has "env", "pre", "transaction" keys)
	for _, testData := range raw {
		var test struct {
			Env         json.RawMessage `json:"env"`
			Pre         json.RawMessage `json:"pre"`
			Transaction json.RawMessage `json:"transaction"`
		}
		if err := json.Unmarshal(testData, &test); err != nil {
			continue
		}
		// If we found env, pre, and transaction, it's likely a valid state test
		if test.Env != nil && test.Pre != nil && test.Transaction != nil {
			return true
		}
	}

	return false
}

// isSupportedFork checks if the fork is supported by go-ethereum
func isSupportedFork(fork string) bool {
	_, ok := tests.Forks[fork]
	return ok
}

// GetSupportedForks returns a list of supported fork names
func GetSupportedForks() []string {
	forks := make([]string, 0, len(tests.Forks))
	for fork := range tests.Forks {
		forks = append(forks, fork)
	}
	return forks
}

// FilterSeedsByFork filters seeds to only include those with tests for specified fork
func FilterSeedsByFork(seeds [][]byte, targetFork string) [][]byte {
	var filtered [][]byte

	for _, seed := range seeds {
		var stateTests map[string]tests.StateTest
		if err := json.Unmarshal(seed, &stateTests); err != nil {
			continue
		}

		for _, test := range stateTests {
			for _, subtest := range test.Subtests() {
				if subtest.Fork == targetFork {
					filtered = append(filtered, seed)
					break
				}
			}
		}
	}

	return filtered
}

// GenerateMinimalSeed generates a minimal valid state test seed
func GenerateMinimalSeed() []byte {
	seed := `{
  "minimalTest": {
    "env": {
      "currentCoinbase": "0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba",
      "currentDifficulty": "0x20000",
      "currentGasLimit": "0x5f5e100",
      "currentNumber": "0x01",
      "currentTimestamp": "0x03e8",
      "previousHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
      "currentBaseFee": "0x0a"
    },
    "pre": {
      "0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b": {
        "balance": "0x0de0b6b3a7640000",
        "code": "0x",
        "nonce": "0x00",
        "storage": {}
      },
      "0xcccccccccccccccccccccccccccccccccccccccc": {
        "balance": "0x00",
        "code": "0x600160015500",
        "nonce": "0x00",
        "storage": {}
      }
    },
    "transaction": {
      "data": ["0x"],
      "gasLimit": ["0x5f5e100"],
      "gasPrice": "0x0a",
      "nonce": "0x00",
      "secretKey": "0x45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8",
      "to": "0xcccccccccccccccccccccccccccccccccccccccc",
      "value": ["0x00"]
    },
    "post": {
      "London": [
        {
          "hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
          "indexes": {"data": 0, "gas": 0, "value": 0}
        }
      ]
    }
  }
}`
	return []byte(seed)
}

// EmbeddedSeeds returns a set of built-in minimal seeds for bootstrapping
func EmbeddedSeeds() [][]byte {
	return [][]byte{
		GenerateMinimalSeed(),
		// Add more embedded seeds as needed
	}
}
