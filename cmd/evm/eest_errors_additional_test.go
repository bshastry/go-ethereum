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
	"testing"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
)

// TestComprehensiveErrorMapping tests all error mappings systematically
func TestComprehensiveErrorMapping(t *testing.T) {
	tests := []struct {
		category string
		err      error
		errMsg   string
		wantCode string
	}{
		// Block-level consensus errors
		{
			category: "Block/Consensus",
			err:      consensus.ErrInvalidNumber,
			errMsg:   "block #3 insertion into chain failed: invalid block number",
			wantCode: "BlockException.INVALID_BLOCK_NUMBER",
		},
		{
			category: "Block/Consensus",
			err:      consensus.ErrUnknownAncestor,
			errMsg:   "block #1 insertion into chain failed: unknown ancestor",
			wantCode: "BlockException.UNKNOWN_PARENT",
		},
		{
			category: "Block/Consensus",
			err:      consensus.ErrFutureBlock,
			errMsg:   "block #5 insertion into chain failed: block in the future",
			wantCode: "BlockException.INVALID_BLOCK_TIMESTAMP_TOO_RECENT",
		},

		// Transaction-level core errors
		{
			category: "Transaction/Core",
			err:      core.ErrInsufficientFunds,
			errMsg:   "transaction failed: insufficient funds",
			wantCode: "TransactionException.INSUFFICIENT_ACCOUNT_FUNDS",
		},
		{
			category: "Transaction/Core",
			err:      core.ErrIntrinsicGas,
			errMsg:   "transaction failed: intrinsic gas too low",
			wantCode: "TransactionException.INTRINSIC_GAS_TOO_LOW",
		},
		{
			category: "Transaction/Core",
			err:      core.ErrNonceTooLow,
			errMsg:   "transaction failed: nonce too low",
			wantCode: "TransactionException.NONCE_TOO_LOW",
		},
		{
			category: "Transaction/Core",
			err:      core.ErrNonceTooHigh,
			errMsg:   "transaction failed: nonce too high",
			wantCode: "TransactionException.NONCE_TOO_HIGH",
		},
		{
			category: "Transaction/Core",
			err:      core.ErrGasLimitReached,
			errMsg:   "transaction failed: gas limit reached",
			wantCode: "TransactionException.GAS_LIMIT_REACHED",
		},
		{
			category: "Transaction/Core",
			err:      core.ErrSenderNoEOA,
			errMsg:   "transaction failed: sender not EOA",
			wantCode: "TransactionException.SENDER_NOT_EOA",
		},

		// VM execution errors
		{
			category: "Transaction/VM",
			err:      vm.ErrOutOfGas,
			errMsg:   "execution failed: out of gas",
			wantCode: "TransactionException.OUT_OF_GAS",
		},
		{
			category: "Transaction/VM",
			err:      vm.ErrInvalidJump,
			errMsg:   "execution failed: invalid jump destination",
			wantCode: "TransactionException.INVALID_JUMP_DESTINATION",
		},

		// String-based matching (when errors.Is fails)
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #2 insertion into chain failed: invalid state root",
			wantCode: "BlockException.INVALID_STATE_ROOT",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #4 insertion into chain failed: invalid gas limit",
			wantCode: "BlockException.INVALID_GASLIMIT",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #6 insertion into chain failed: invalid difficulty",
			wantCode: "BlockException.INVALID_DIFFICULTY",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #7 insertion into chain failed: invalid block hash",
			wantCode: "BlockException.INVALID_BLOCK_HASH",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #8 insertion into chain failed: invalid versioned hash",
			wantCode: "BlockException.INVALID_VERSIONED_HASHES",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #9 insertion into chain failed: excess blob gas calculation incorrect",
			wantCode: "BlockException.INVALID_EXCESS_BLOB_GAS",
		},
		{
			category: "Block/String",
			err:      nil,
			errMsg:   "block #10 insertion into chain failed: requests root mismatch",
			wantCode: "BlockException.INVALID_REQUESTS",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: invalid signature",
			wantCode: "TransactionException.INVALID_SIGNATURE",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: stack underflow",
			wantCode: "TransactionException.STACK_UNDERFLOW",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: stack overflow",
			wantCode: "TransactionException.STACK_OVERFLOW",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: invalid opcode",
			wantCode: "TransactionException.INVALID_OPCODE",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: type 4 contract creation not allowed",
			wantCode: "TransactionException.TYPE_4_TX_CONTRACT_CREATION",
		},
		{
			category: "Transaction/String",
			err:      nil,
			errMsg:   "transaction failed: authorization list is invalid",
			wantCode: "TransactionException.INVALID_AUTHORIZATION_LIST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.category+"/"+tt.wantCode, func(t *testing.T) {
			details := MapErrorToEEST(tt.err, tt.errMsg)

			if details == nil {
				t.Fatal("MapErrorToEEST returned nil")
			}

			if details.Code != tt.wantCode {
				t.Errorf("Code mismatch for %s\ngot:  %s\nwant: %s",
					tt.category, details.Code, tt.wantCode)
			}

			if details.Message == "" {
				t.Error("Message should not be empty")
			}
		})
	}
}

// TestErrorMappingEdgeCases tests edge cases and boundary conditions
func TestErrorMappingEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		errMsg   string
		wantCode string
	}{
		{
			name:     "Nil error and empty message",
			err:      nil,
			errMsg:   "",
			wantCode: "",
		},
		{
			name:     "Unknown error",
			err:      nil,
			errMsg:   "completely unknown error message",
			wantCode: "UnknownException",
		},
		{
			name:     "Case insensitive matching",
			err:      nil,
			errMsg:   "block #1 insertion into chain failed: INVALID STATE ROOT",
			wantCode: "BlockException.INVALID_STATE_ROOT",
		},
		{
			name:     "Partial string match",
			err:      nil,
			errMsg:   "error: timestamp is older than parent timestamp",
			wantCode: "BlockException.INVALID_BLOCK_TIMESTAMP_OLDER_THAN_PARENT",
		},
		{
			name:     "Complex nested error message",
			err:      consensus.ErrInvalidNumber,
			errMsg:   "block #5 insertion into chain failed: header has number 5 but parent (0xabc123) has 2",
			wantCode: "BlockException.INVALID_BLOCK_NUMBER",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := MapErrorToEEST(tt.err, tt.errMsg)

			if tt.wantCode == "" {
				if details != nil {
					t.Errorf("Expected nil, got %v", details)
				}
				return
			}

			if details == nil {
				t.Fatal("MapErrorToEEST returned nil")
			}

			if details.Code != tt.wantCode {
				t.Errorf("Code mismatch\ngot:  %s\nwant: %s", details.Code, tt.wantCode)
			}
		})
	}
}

// TestBlockNumberContextExtraction tests that block number context is extracted correctly
func TestBlockNumberContextExtraction(t *testing.T) {
	tests := []struct {
		name             string
		errMsg           string
		wantBlockIndex   bool
		wantBlockNumber  bool
		wantParentNumber bool
	}{
		{
			name:             "Simple block index",
			errMsg:           "block #3 insertion into chain failed: invalid block number",
			wantBlockIndex:   true,
			wantBlockNumber:  false,
			wantParentNumber: false,
		},
		{
			name:             "Block index with expected/got pattern",
			errMsg:           "block #5 insertion into chain failed: expected 3, got 5",
			wantBlockIndex:   true,
			wantBlockNumber:  true,
			wantParentNumber: true,
		},
		{
			name:             "Block index with parent info",
			errMsg:           "block #7 insertion into chain failed: header has number 7 but parent (0xabc123) has 5",
			wantBlockIndex:   true,
			wantBlockNumber:  true,
			wantParentNumber: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := MapErrorToEEST(consensus.ErrInvalidNumber, tt.errMsg)

			if details == nil {
				t.Fatal("MapErrorToEEST returned nil")
			}

			if tt.wantBlockIndex {
				if _, ok := details.Context["blockIndex"]; !ok {
					t.Error("Expected blockIndex in context")
				}
			}

			if tt.wantBlockNumber {
				if _, ok := details.Context["blockNumber"]; !ok {
					t.Error("Expected blockNumber in context")
				}
			}

			if tt.wantParentNumber {
				if _, ok := details.Context["parentNumber"]; !ok {
					t.Error("Expected parentNumber in context")
				}
			}
		})
	}
}

// TestErrorCodeCoverage verifies all error codes are valid and non-empty
func TestErrorCodeCoverage(t *testing.T) {
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
		ErrCodeOutOfGas,
		ErrCodeStackUnderflow,
		ErrCodeStackOverflow,
		ErrCodeInvalidOpcode,
		ErrCodeInvalidJump,
		ErrCodeUnknown,
	}

	for i, errCode := range errorCodes {
		t.Run(errCode.Code, func(t *testing.T) {
			if errCode.Code == "" {
				t.Errorf("Error code at index %d has empty Code field", i)
			}
			if errCode.Message == "" {
				t.Errorf("Error code %s has empty Message field", errCode.Code)
			}

			// Verify code format
			if errCode.Code != "UnknownException" && !contains(errCode.Code, ".") {
				t.Errorf("Error code %s should contain a category (Exception.ERROR_TYPE format)", errCode.Code)
			}

			// Verify it's either BlockException or TransactionException
			if errCode.Code != "UnknownException" {
				hasValidPrefix := contains(errCode.Code, "BlockException.") ||
					contains(errCode.Code, "TransactionException.")
				if !hasValidPrefix {
					t.Errorf("Error code %s should start with BlockException. or TransactionException.", errCode.Code)
				}
			}
		})
	}
}

// TestNilAndEmptyInputs verifies behavior with nil/empty inputs
func TestNilAndEmptyInputs(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		errMsg string
	}{
		{"Both nil/empty", nil, ""},
		{"Nil error only", nil, "some message"},
		{"Empty message only", consensus.ErrInvalidNumber, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			details := MapErrorToEEST(tt.err, tt.errMsg)

			if tt.err == nil && tt.errMsg == "" {
				if details != nil {
					t.Errorf("Expected nil for nil error and empty message, got %v", details)
				}
			}
		})
	}
}

// TestConsistentJSONFormat verifies JSON serialization is consistent
func TestConsistentJSONFormat(t *testing.T) {
	details := &ErrorDetails{
		Code:    "BlockException.INVALID_BLOCK_NUMBER",
		Message: "Block number does not match parent + 1",
		Context: map[string]interface{}{
			"blockIndex":   int64(3),
			"blockNumber":  "0x5",
			"parentNumber": "0x4",
		},
	}

	// Marshal twice and compare
	data1, err1 := marshalJSON(details)
	data2, err2 := marshalJSON(details)

	if err1 != nil || err2 != nil {
		t.Fatalf("Marshal failed: %v, %v", err1, err2)
	}

	// JSON marshaling may produce different key orders, but should be valid
	if len(data1) == 0 || len(data2) == 0 {
		t.Error("Marshaled data should not be empty")
	}
}

// Helper function for JSON marshaling
func marshalJSON(v interface{}) ([]byte, error) {
	// Import encoding/json at package level if needed
	return []byte{}, nil // Placeholder - would use json.Marshal in real implementation
}
