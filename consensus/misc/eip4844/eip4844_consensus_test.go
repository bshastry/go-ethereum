// Copyright 2025 The go-ethereum Authors
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

package eip4844

import (
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TestVerifyEIP4844Header_IncorrectExcessBlobGas tests that VerifyEIP4844Header
// correctly rejects blocks with incorrect excessBlobGas values.
// This is a consensus-critical validation - incorrect values can cause chain splits.
func TestVerifyEIP4844Header_IncorrectExcessBlobGas(t *testing.T) {
	config := &params.ChainConfig{
		LondonBlock: big.NewInt(0),
		CancunTime:  new(uint64), // Enable Cancun (EIP-4844) from genesis
	}
	*config.CancunTime = 0

	// Set up blob schedule config for Cancun (3 target, 6 max)
	config.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: &params.BlobConfig{
			Target:         3,
			Max:            6,
			UpdateFraction: 3338477, // Cancun update fraction
		},
	}

	tests := []struct {
		name              string
		parentExcess      uint64
		parentUsed        uint64
		childExcess       uint64
		expectError       bool
		expectedErrorMsg  string
	}{
		{
			name:         "correct: parent(0,131072) -> child(0) when excess < target",
			parentExcess: 0,
			parentUsed:   params.BlobTxBlobGasPerBlob, // 1 blob = 131072 gas
			childExcess:  0,                            // 0 + 131072 = 131072 < target(393216), so 0
			expectError:  false,
		},
		{
			name:         "correct: parent(0,786432) -> child(0) when excess == target",
			parentExcess: 0,
			parentUsed:   params.BlobTxBlobGasPerBlob * 6, // 6 blobs = 786432 gas = target
			childExcess:  0,                                // 0 + 786432 = 786432, 786432 - 786432 = 0
			expectError:  false,
		},
		{
			name:         "correct: parent(0,917504) -> child(131072) when excess > target",
			parentExcess: 0,
			parentUsed:   params.BlobTxBlobGasPerBlob * 7, // 7 blobs = 917504 gas
			childExcess:  131072,                           // 0 + 917504 = 917504, 917504 - 786432 = 131072
			expectError:  false,
		},
		{
			name:             "INCORRECT: parent(0,786432) -> child(131072) - should be 0",
			parentExcess:     0,
			parentUsed:       params.BlobTxBlobGasPerBlob * 6, // 6 blobs
			childExcess:      131072,                           // WRONG! Should be 0
			expectError:      true,
			expectedErrorMsg: "invalid excessBlobGas",
		},
		{
			name:             "INCORRECT: parent(0,917504) -> child(0) - should be 131072",
			parentExcess:     0,
			parentUsed:       params.BlobTxBlobGasPerBlob * 7, // 7 blobs
			childExcess:      0,                                // WRONG! Should be 131072
			expectError:      true,
			expectedErrorMsg: "invalid excessBlobGas",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := &types.Header{
				Number:        big.NewInt(1),
				Time:          100,
				ExcessBlobGas: &tt.parentExcess,
				BlobGasUsed:   &tt.parentUsed,
			}

			child := &types.Header{
				Number:        big.NewInt(2),
				Time:          200,
				ExcessBlobGas: &tt.childExcess,
				BlobGasUsed:   new(uint64), // 0 blobs used in child
			}

			err := VerifyEIP4844Header(config, parent, child)

			if tt.expectError {
				if err == nil {
					t.Fatalf("CONSENSUS BUG DETECTED: Expected error containing '%s' but got nil! "+
						"Block with incorrect excessBlobGas was accepted. "+
						"Parent had excess=%d used=%d, child has excess=%d but should have %d",
						tt.expectedErrorMsg, tt.parentExcess, tt.parentUsed, tt.childExcess,
						CalcExcessBlobGas(config, parent, child.Time))
				}
				if tt.expectedErrorMsg != "" && !strings.Contains(err.Error(), tt.expectedErrorMsg) {
					t.Fatalf("Expected error containing '%s' but got: %v", tt.expectedErrorMsg, err)
				}
				t.Logf("✓ Correctly rejected invalid block: %v", err)
			} else {
				if err != nil {
					t.Fatalf("Expected no error but got: %v", err)
				}
			}
		})
	}
}
