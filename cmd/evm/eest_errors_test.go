// Copyright 2025 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
)

func TestMapErrorToEEST(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		errMsg      string
		wantCode    string
		wantContext bool
	}{
		{
			name:        "InvalidBlockNumber consensus error",
			err:         consensus.ErrInvalidNumber,
			errMsg:      "block #3 insertion into chain failed: invalid block number",
			wantCode:    "BlockException.INVALID_BLOCK_NUMBER",
			wantContext: true,
		},
		{
			name:     "UnknownAncestor consensus error",
			err:      consensus.ErrUnknownAncestor,
			errMsg:   "block #1 insertion into chain failed: unknown ancestor",
			wantCode: "BlockException.UNKNOWN_PARENT",
		},
		{
			name:     "FutureBlock consensus error",
			err:      consensus.ErrFutureBlock,
			errMsg:   "block #5 insertion into chain failed: block in the future",
			wantCode: "BlockException.INVALID_BLOCK_TIMESTAMP_TOO_RECENT",
		},
		{
			name:     "InsufficientFunds core error",
			err:      core.ErrInsufficientFunds,
			errMsg:   "transaction failed: insufficient funds",
			wantCode: "TransactionException.INSUFFICIENT_ACCOUNT_FUNDS",
		},
		{
			name:     "IntrinsicGas core error",
			err:      core.ErrIntrinsicGas,
			errMsg:   "transaction failed: intrinsic gas too low",
			wantCode: "TransactionException.INTRINSIC_GAS_TOO_LOW",
		},
		{
			name:     "NonceTooLow core error",
			err:      core.ErrNonceTooLow,
			errMsg:   "transaction failed: nonce too low",
			wantCode: "TransactionException.NONCE_TOO_LOW",
		},
		{
			name:     "OutOfGas VM error",
			err:      vm.ErrOutOfGas,
			errMsg:   "execution failed: out of gas",
			wantCode: "TransactionException.OUT_OF_GAS",
		},
		{
			name:     "InvalidJump VM error",
			err:      vm.ErrInvalidJump,
			errMsg:   "execution failed: invalid jump destination",
			wantCode: "TransactionException.INVALID_JUMP_DESTINATION",
		},
		{
			name:     "String-based matching for state root",
			err:      nil,
			errMsg:   "block #2 insertion into chain failed: invalid state root",
			wantCode: "BlockException.INVALID_STATE_ROOT",
		},
		{
			name:     "String-based matching for gas limit",
			err:      nil,
			errMsg:   "block #4 insertion into chain failed: invalid gas limit",
			wantCode: "BlockException.INVALID_GAS_LIMIT",
		},
		{
			name:     "MaxInitCodeSizeExceeded core error",
			err:      core.ErrMaxInitCodeSizeExceeded,
			errMsg:   "transaction failed: max initcode size exceeded",
			wantCode: "TransactionException.INITCODE_SIZE_EXCEEDED",
		},
		{
			name:     "BlobTxCreate core error",
			err:      core.ErrBlobTxCreate,
			errMsg:   "transaction failed: blob transaction of type create",
			wantCode: "TransactionException.BLOB_CREATE_CONTRACT",
		},
		{
			name:     "FeeCapTooLow core error",
			err:      core.ErrFeeCapTooLow,
			errMsg:   "transaction failed: max fee per gas less than block base fee",
			wantCode: "TransactionException.TX_FEE_CAP_LESS_THAN_BLOCKS",
		},
		{
			name:     "String-based matching for initcode size",
			err:      nil,
			errMsg:   "block #1 insertion failed: max initcode size exceeded: code size 49153 limit 49152",
			wantCode: "TransactionException.INITCODE_SIZE_EXCEEDED",
		},
		{
			name:     "String-based matching for blob create",
			err:      nil,
			errMsg:   "transaction failed: blob transaction cannot create contract",
			wantCode: "TransactionException.BLOB_CREATE_CONTRACT",
		},
		{
			name:     "String-based matching for fee cap less than base",
			err:      nil,
			errMsg:   "transaction failed: fee cap less than base fee",
			wantCode: "TransactionException.TX_FEE_CAP_LESS_THAN_BLOCKS",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := MapErrorToEEST(tt.err, tt.errMsg)

			if details == nil {
				t.Fatal("MapErrorToEEST returned nil")
			}

			if details.Code != tt.wantCode {
				t.Errorf("Code mismatch\ngot:  %s\nwant: %s", details.Code, tt.wantCode)
			}

			if details.Message == "" {
				t.Error("Message should not be empty")
			}

			if tt.wantContext && (details.Context == nil || len(details.Context) == 0) {
				t.Error("Expected context to be populated")
			}

			// Verify JSON serialization works
			if _, err := json.Marshal(details); err != nil {
				t.Errorf("Failed to marshal ErrorDetails: %v", err)
			}
		})
	}
}

func TestMapErrorToEESTWithBlockContext(t *testing.T) {
	// Test that block number context is extracted correctly
	testCases := []struct {
		errMsg           string
		wantBlockIndex   bool
		wantBlockNumber  bool
		wantParentNumber bool
	}{
		{
			errMsg:           "block #3 insertion into chain failed: invalid block number",
			wantBlockIndex:   true,
			wantBlockNumber:  false,
			wantParentNumber: false,
		},
		{
			errMsg:           "block #5 insertion into chain failed: expected 3, got 5",
			wantBlockIndex:   true,
			wantBlockNumber:  true,
			wantParentNumber: true,
		},
	}

	for i, tc := range testCases {
		t.Run(fmt.Sprintf("case_%d", i), func(t *testing.T) {
			details := MapErrorToEEST(consensus.ErrInvalidNumber, tc.errMsg)

			if tc.wantBlockIndex {
				if _, ok := details.Context["blockIndex"]; !ok {
					t.Error("Expected blockIndex in context")
				}
			}

			if tc.wantBlockNumber {
				if _, ok := details.Context["blockNumber"]; !ok {
					t.Error("Expected blockNumber in context")
				}
			}

			if tc.wantParentNumber {
				if _, ok := details.Context["parentNumber"]; !ok {
					t.Error("Expected parentNumber in context")
				}
			}
		})
	}
}

func TestTraceEndDetailsMarshaling(t *testing.T) {
	// Test that the complete traceEndDetails structure serializes correctly
	lastValidBlock := uint64(2)
	details := traceEndDetails{
		Name:               "test_case",
		Pass:               false,
		Fork:               "Osaka",
		Error:              "BlockException.INVALID_BLOCK_NUMBER",
		LastValidStateRoot: "0xabc2b4b861acae7b3ef896780803ea7a05bdc8a941d1bf949c8a5e6bf5d350a3",
		LastValidBlock:     &lastValidBlock,
	}

	marker := traceEndMarker{TestEnd: details}

	data, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal traceEndMarker: %v", err)
	}

	t.Logf("Marshaled output:\n%s", string(data))

	// Verify it unmarshals back
	var unmarshaled traceEndMarker
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Verify key fields
	if unmarshaled.TestEnd.Name != details.Name {
		t.Errorf("Name mismatch: got %s, want %s", unmarshaled.TestEnd.Name, details.Name)
	}

	if unmarshaled.TestEnd.Error != details.Error {
		t.Errorf("Error mismatch: got %s, want %s",
			unmarshaled.TestEnd.Error, details.Error)
	}

	if unmarshaled.TestEnd.LastValidStateRoot != details.LastValidStateRoot {
		t.Errorf("LastValidStateRoot mismatch: got %s, want %s",
			unmarshaled.TestEnd.LastValidStateRoot, details.LastValidStateRoot)
	}

	if unmarshaled.TestEnd.LastValidBlock == nil || details.LastValidBlock == nil {
		t.Fatal("LastValidBlock should not be nil")
	}
	if *unmarshaled.TestEnd.LastValidBlock != *details.LastValidBlock {
		t.Errorf("LastValidBlock mismatch: got %d, want %d",
			*unmarshaled.TestEnd.LastValidBlock, *details.LastValidBlock)
	}
}

func TestEESTErrorCodeCoverage(t *testing.T) {
	// Verify that all EEST error codes have non-empty messages
	errorCodes := []EESTErrorCode{
		ErrCodeInvalidBlockNumber,
		ErrCodeInvalidTimestamp,
		ErrCodeInvalidDifficulty,
		ErrCodeInvalidGasLimit,
		ErrCodeInvalidStateRoot,
		ErrCodeInvalidBlockHash,
		ErrCodeInvalidVersionedHashes,
		ErrCodeInvalidExcessBlobGas,
		ErrCodeInvalidRequests,
		ErrCodeUnknownAncestor,
		ErrCodeFutureBlock,
		ErrCodeInsufficientFunds,
		ErrCodeIntrinsicGas,
		ErrCodeNonceTooLow,
		ErrCodeNonceTooHigh,
		ErrCodeSenderNotEOA,
		ErrCodeInvalidSignature,
		ErrCodeGasLimitReached,
		ErrCodeMaxFeePerGas,
		ErrCodeMaxPriorityFee,
		ErrCodeType4ContractCreation,
		ErrCodeInvalidAuthorizationList,
		ErrCodeInitcodeSizeExceeded,
		ErrCodeBlobCreateContract,
		ErrCodeFeeCapLessThanBlocks,
		ErrCodeOutOfGas,
		ErrCodeStackUnderflow,
		ErrCodeStackOverflow,
		ErrCodeInvalidOpcode,
		ErrCodeInvalidJump,
		ErrCodeUnknown,
	}

	for _, errCode := range errorCodes {
		if errCode.Code == "" {
			t.Errorf("Error code has empty Code field")
		}
		if errCode.Message == "" {
			t.Errorf("Error code %s has empty Message field", errCode.Code)
		}

		// Verify code format (should contain a dot for categorization)
		if errCode.Code != "UnknownException" && !contains(errCode.Code, ".") {
			t.Errorf("Error code %s should contain a category (Exception.ERROR_TYPE format)", errCode.Code)
		}
	}
}

func contains(s, substr string) bool {
	for i := 0; i < len(s)-len(substr)+1; i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
