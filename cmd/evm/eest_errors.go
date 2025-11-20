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
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

// ErrorDetails provides structured error information using EEST exception codes
// for machine-parsable, cross-client compatible error reporting.
// Fields ordered alphabetically for consistent JSON output.
type ErrorDetails struct {
	Code    string                 `json:"code"`
	Context map[string]interface{} `json:"context,omitempty"`
	Message string                 `json:"message"`
}

// EESTErrorCode represents an Ethereum Execution Spec Test error code.
type EESTErrorCode struct {
	Code    string // e.g., "BlockException.INVALID_BLOCK_NUMBER"
	Message string // Human-readable message
}

// Common EEST error codes mapped from Go errors
var (
	// Block-level errors
	ErrCodeInvalidBlockNumber = EESTErrorCode{
		Code:    "BlockException.INVALID_BLOCK_NUMBER",
		Message: "Block number does not match parent + 1",
	}
	ErrCodeInvalidTimestamp = EESTErrorCode{
		Code:    "BlockException.INVALID_BLOCK_TIMESTAMP_OLDER_THAN_PARENT",
		Message: "Block timestamp must be greater than parent timestamp",
	}
	ErrCodeInvalidDifficulty = EESTErrorCode{
		Code:    "BlockException.INVALID_DIFFICULTY",
		Message: "Block difficulty calculation incorrect",
	}
	ErrCodeInvalidGasLimit = EESTErrorCode{
		Code:    "BlockException.INVALID_GASLIMIT",
		Message: "Gas limit exceeds maximum allowed change",
	}
	ErrCodeGasLimitTooBig = EESTErrorCode{
		Code:    "BlockException.GASLIMIT_TOO_BIG",
		Message: "Gas limit exceeds maximum value (0x7fffffffffffffff)",
	}
	ErrCodeInvalidStateRoot = EESTErrorCode{
		Code:    "BlockException.INVALID_STATE_ROOT",
		Message: "State root does not match computed value",
	}
	ErrCodeInvalidBlockHash = EESTErrorCode{
		Code:    "BlockException.INVALID_BLOCK_HASH",
		Message: "Block hash does not match computed value",
	}
	ErrCodeInvalidVersionedHashes = EESTErrorCode{
		Code:    "BlockException.INVALID_VERSIONED_HASHES",
		Message: "Blob versioned hashes do not match",
	}
	ErrCodeInvalidExcessBlobGas = EESTErrorCode{
		Code:    "BlockException.INCORRECT_EXCESS_BLOB_GAS",
		Message: "Excess blob gas calculation incorrect",
	}
	ErrCodeInvalidRequests = EESTErrorCode{
		Code:    "BlockException.INVALID_REQUESTS",
		Message: "EIP-7685 requests root does not match",
	}
	ErrCodeInvalidBaseFee = EESTErrorCode{
		Code:    "BlockException.INVALID_BASEFEE_PER_GAS",
		Message: "Block base fee does not match expected calculation",
	}
	ErrCodeUnknownAncestor = EESTErrorCode{
		Code:    "BlockException.UNKNOWN_PARENT",
		Message: "Parent block not found in chain",
	}
	ErrCodeFutureBlock = EESTErrorCode{
		Code:    "BlockException.INVALID_BLOCK_TIMESTAMP_TOO_RECENT",
		Message: "Block timestamp is in the future",
	}

	// Transaction-level errors
	ErrCodeInsufficientFunds = EESTErrorCode{
		Code:    "TransactionException.INSUFFICIENT_ACCOUNT_FUNDS",
		Message: "Sender account balance insufficient for transfer",
	}
	ErrCodeIntrinsicGas = EESTErrorCode{
		Code:    "TransactionException.INTRINSIC_GAS_TOO_LOW",
		Message: "Gas limit below intrinsic cost",
	}
	ErrCodeNonceTooLow = EESTErrorCode{
		Code:    "TransactionException.NONCE_TOO_LOW",
		Message: "Transaction nonce below account nonce",
	}
	ErrCodeNonceTooHigh = EESTErrorCode{
		Code:    "TransactionException.NONCE_TOO_HIGH",
		Message: "Transaction nonce too far ahead",
	}
	ErrCodeSenderNotEOA = EESTErrorCode{
		Code:    "TransactionException.SENDER_NOT_EOA",
		Message: "Transaction sender must be externally owned account",
	}
	ErrCodeInvalidSignature = EESTErrorCode{
		Code:    "TransactionException.INVALID_SIGNATURE",
		Message: "Transaction signature verification failed",
	}
	ErrCodeGasLimitReached = EESTErrorCode{
		Code:    "TransactionException.GAS_LIMIT_REACHED",
		Message: "Transaction exceeds block gas limit",
	}
	ErrCodeMaxFeePerGas = EESTErrorCode{
		Code:    "TransactionException.GAS_PRICE_GREATER_THAN_MAX_FEE_PER_GAS",
		Message: "Gas price exceeds max fee per gas",
	}
	ErrCodeMaxPriorityFee = EESTErrorCode{
		Code:    "TransactionException.PRIORITY_GREATER_THAN_MAX_FEE_PER_GAS",
		Message: "Priority fee exceeds max fee per gas",
	}
	ErrCodeType4ContractCreation = EESTErrorCode{
		Code:    "TransactionException.TYPE_4_TX_CONTRACT_CREATION",
		Message: "EIP-7702 transaction cannot create contracts",
	}
	ErrCodeInvalidAuthorizationList = EESTErrorCode{
		Code:    "TransactionException.INVALID_AUTHORIZATION_LIST",
		Message: "Authorization list is malformed or invalid",
	}
	ErrCodeInitcodeSizeExceeded = EESTErrorCode{
		Code:    "TransactionException.INITCODE_SIZE_EXCEEDED",
		Message: "EIP-3860: Initcode size exceeds maximum allowed",
	}
	ErrCodeBlobCreateContract = EESTErrorCode{
		Code:    "TransactionException.BLOB_CREATE_CONTRACT",
		Message: "EIP-4844: Blob transaction cannot create contracts",
	}
	ErrCodeFeeCapLessThanBlocks = EESTErrorCode{
		Code:    "TransactionException.TX_FEE_CAP_LESS_THAN_BLOCKS",
		Message: "Transaction max fee per gas less than block base fee",
	}

	// VM execution errors
	ErrCodeOutOfGas = EESTErrorCode{
		Code:    "TransactionException.OUT_OF_GAS",
		Message: "Transaction execution ran out of gas",
	}
	ErrCodeStackUnderflow = EESTErrorCode{
		Code:    "TransactionException.STACK_UNDERFLOW",
		Message: "Stack underflow during execution",
	}
	ErrCodeStackOverflow = EESTErrorCode{
		Code:    "TransactionException.STACK_OVERFLOW",
		Message: "Stack overflow during execution",
	}
	ErrCodeInvalidOpcode = EESTErrorCode{
		Code:    "TransactionException.INVALID_OPCODE",
		Message: "Invalid opcode encountered",
	}
	ErrCodeInvalidJump = EESTErrorCode{
		Code:    "TransactionException.INVALID_JUMP_DESTINATION",
		Message: "Invalid jump destination",
	}

	// Generic/Unknown
	ErrCodeUnknown = EESTErrorCode{
		Code:    "UnknownException",
		Message: "Unknown error occurred",
	}
)

// blockInsertionPattern matches errors like "block #3 insertion into chain failed: invalid block number"
var blockInsertionPattern = regexp.MustCompile(`block #(\d+) insertion(?: into chain)? failed: (.+)`)

// negativeTestPattern matches errors from negative test validation like:
// "block (index 8) insertion should have failed due to: TransactionException.NONCE_MISMATCH_TOO_LOW"
var negativeTestPattern = regexp.MustCompile(`block \(index (\d+)\) insertion should have failed due to: ([A-Za-z._]+)`)

// blockNumberPattern extracts context from block number errors
var blockNumberPattern = regexp.MustCompile(`expected (\d+), got (\d+)|header has number (\d+) but parent \((\w+)\) has (\d+)`)

// MapErrorToEEST converts a Go error to EEST error details with structured context.
// It analyzes the error chain and extracts relevant context information.
func MapErrorToEEST(err error, errMsg string) *ErrorDetails {
	if err == nil && errMsg == "" {
		return nil
	}

	// First, check if this is a negative test validation error that already contains
	// the expected EEST error code. These errors occur when a block was supposed to
	// fail validation but didn't, and the error message includes the expected error code.
	if matches := negativeTestPattern.FindStringSubmatch(errMsg); len(matches) == 3 {
		blockIndex, _ := strconv.ParseInt(matches[1], 10, 64)
		eestCode := matches[2]

		context := make(map[string]interface{})
		context["blockIndex"] = blockIndex

		// Return the EEST error code directly from the test expectation
		return &ErrorDetails{
			Code:    eestCode,
			Message: fmt.Sprintf("Block insertion should have failed with: %s", eestCode),
			Context: context,
		}
	}

	// Extract block index and base error from insertion message
	var (
		blockIndex int64 = -1
		baseErr    string
	)
	if matches := blockInsertionPattern.FindStringSubmatch(errMsg); len(matches) > 0 {
		if idx, parseErr := strconv.ParseInt(matches[1], 10, 64); parseErr == nil {
			blockIndex = idx
		}
		if len(matches) > 2 {
			baseErr = matches[2]
		}
	}

	// Initialize context
	context := make(map[string]interface{})
	if blockIndex >= 0 {
		context["blockIndex"] = blockIndex
	}

	// Map error to EEST code
	var eestErr EESTErrorCode

	// Try exact error matching first
	switch {
	// Consensus errors
	case errors.Is(err, consensus.ErrInvalidNumber):
		eestErr = ErrCodeInvalidBlockNumber
		extractBlockNumberContext(err, errMsg, context)

	case errors.Is(err, consensus.ErrUnknownAncestor):
		eestErr = ErrCodeUnknownAncestor

	case errors.Is(err, consensus.ErrFutureBlock):
		eestErr = ErrCodeFutureBlock

	// Core errors
	case errors.Is(err, core.ErrNonceTooLow):
		eestErr = ErrCodeNonceTooLow

	case errors.Is(err, core.ErrNonceTooHigh):
		eestErr = ErrCodeNonceTooHigh

	case errors.Is(err, core.ErrInsufficientFunds):
		eestErr = ErrCodeInsufficientFunds

	case errors.Is(err, core.ErrIntrinsicGas):
		eestErr = ErrCodeIntrinsicGas

	case errors.Is(err, core.ErrGasLimitReached):
		eestErr = ErrCodeGasLimitReached

	case errors.Is(err, core.ErrSenderNoEOA):
		eestErr = ErrCodeSenderNotEOA

	case errors.Is(err, core.ErrMaxInitCodeSizeExceeded):
		eestErr = ErrCodeInitcodeSizeExceeded

	case errors.Is(err, core.ErrBlobTxCreate):
		eestErr = ErrCodeBlobCreateContract

	case errors.Is(err, core.ErrFeeCapTooLow):
		eestErr = ErrCodeFeeCapLessThanBlocks

	// VM errors
	case errors.Is(err, vm.ErrOutOfGas):
		eestErr = ErrCodeOutOfGas

	case errors.Is(err, vm.ErrInvalidJump):
		eestErr = ErrCodeInvalidJump

	default:
		// Try pattern matching on error message
		eestErr = matchErrorByString(baseErr, errMsg)
	}

	// Build result
	details := &ErrorDetails{
		Code:    eestErr.Code,
		Message: eestErr.Message,
	}
	if len(context) > 0 {
		details.Context = context
	}

	return details
}

// matchErrorByString attempts to match errors based on their string representation.
// This is used when errors.Is matching fails (e.g., wrapped errors, dynamic errors).
func matchErrorByString(baseErr, fullMsg string) EESTErrorCode {
	errStr := strings.ToLower(baseErr)
	if errStr == "" {
		errStr = strings.ToLower(fullMsg)
	}

	switch {
	case strings.Contains(errStr, "invalid block number") || strings.Contains(errStr, "invalid number"):
		return ErrCodeInvalidBlockNumber

	case strings.Contains(errStr, "timestamp") && strings.Contains(errStr, "older"):
		return ErrCodeInvalidTimestamp

	case strings.Contains(errStr, "difficulty"):
		return ErrCodeInvalidDifficulty

	case strings.Contains(errStr, "gas limit") || strings.Contains(errStr, "gaslimit"):
		// Distinguish between GASLIMIT_TOO_BIG (exceeds max) and INVALID_GASLIMIT (bounds check)
		if strings.Contains(errStr, "max") {
			return ErrCodeGasLimitTooBig
		}
		return ErrCodeInvalidGasLimit

	case strings.Contains(errStr, "state root") || strings.Contains(errStr, "stateroot"):
		return ErrCodeInvalidStateRoot

	case strings.Contains(errStr, "block hash"):
		return ErrCodeInvalidBlockHash

	case strings.Contains(errStr, "versioned hash") || strings.Contains(errStr, "blob hash"):
		return ErrCodeInvalidVersionedHashes

	case strings.Contains(errStr, "excess blob gas"):
		return ErrCodeInvalidExcessBlobGas

	case strings.Contains(errStr, "requests"):
		return ErrCodeInvalidRequests

	case strings.Contains(errStr, "invalid basefee") || strings.Contains(errStr, "invalid base fee"):
		return ErrCodeInvalidBaseFee

	case strings.Contains(errStr, "unknown ancestor") || strings.Contains(errStr, "unknown parent"):
		return ErrCodeUnknownAncestor

	case strings.Contains(errStr, "future"):
		return ErrCodeFutureBlock

	case strings.Contains(errStr, "insufficient funds") || strings.Contains(errStr, "insufficient balance"):
		return ErrCodeInsufficientFunds

	case strings.Contains(errStr, "intrinsic gas"):
		return ErrCodeIntrinsicGas

	case strings.Contains(errStr, "nonce too low"):
		return ErrCodeNonceTooLow

	case strings.Contains(errStr, "nonce too high"):
		return ErrCodeNonceTooHigh

	case strings.Contains(errStr, "sender not eoa"):
		return ErrCodeSenderNotEOA

	case strings.Contains(errStr, "invalid signature") || strings.Contains(errStr, "signature"):
		return ErrCodeInvalidSignature

	case strings.Contains(errStr, "out of gas"):
		return ErrCodeOutOfGas

	case strings.Contains(errStr, "stack underflow"):
		return ErrCodeStackUnderflow

	case strings.Contains(errStr, "stack overflow"):
		return ErrCodeStackOverflow

	case strings.Contains(errStr, "invalid opcode"):
		return ErrCodeInvalidOpcode

	case strings.Contains(errStr, "invalid jump"):
		return ErrCodeInvalidJump

	case strings.Contains(errStr, "authorization list"):
		return ErrCodeInvalidAuthorizationList

	case strings.Contains(errStr, "type 4") && strings.Contains(errStr, "contract"):
		return ErrCodeType4ContractCreation

	case strings.Contains(errStr, "initcode") && strings.Contains(errStr, "size"):
		return ErrCodeInitcodeSizeExceeded

	case strings.Contains(errStr, "blob") && strings.Contains(errStr, "create"):
		return ErrCodeBlobCreateContract

	case strings.Contains(errStr, "fee cap") && strings.Contains(errStr, "less than") && strings.Contains(errStr, "base"):
		return ErrCodeFeeCapLessThanBlocks

	default:
		return ErrCodeUnknown
	}
}

// extractBlockNumberContext attempts to extract block number context from error messages.
// It handles various error formats from different parts of the codebase.
func extractBlockNumberContext(err error, errMsg string, context map[string]interface{}) {
	// Try to extract numbers from error message
	matches := blockNumberPattern.FindStringSubmatch(errMsg)
	if len(matches) == 0 {
		return
	}

	// Pattern: "expected X, got Y"
	if matches[1] != "" && matches[2] != "" {
		if expected, parseErr := strconv.ParseUint(matches[1], 10, 64); parseErr == nil {
			context["expectedNumber"] = fmt.Sprintf("0x%x", expected)
		}
		if got, parseErr := strconv.ParseUint(matches[2], 10, 64); parseErr == nil {
			context["blockNumber"] = fmt.Sprintf("0x%x", got)
			// Expected number = got - 1 for parent
			if got > 0 {
				context["parentNumber"] = fmt.Sprintf("0x%x", got-1)
			}
		}
	}

	// Pattern: "header has number X but parent (hash) has Y"
	if matches[3] != "" && matches[5] != "" {
		if blockNum, parseErr := strconv.ParseUint(matches[3], 10, 64); parseErr == nil {
			context["blockNumber"] = fmt.Sprintf("0x%x", blockNum)
		}
		if parentNum, parseErr := strconv.ParseUint(matches[5], 10, 64); parseErr == nil {
			context["parentNumber"] = fmt.Sprintf("0x%x", parentNum)
		}
		if matches[4] != "" {
			context["parentHash"] = matches[4]
		}
	}
}

// FormatBlockContext creates structured context for block-related errors.
func FormatBlockContext(block *types.Block, parent *types.Header) map[string]interface{} {
	context := make(map[string]interface{})

	if block != nil {
		context["blockNumber"] = fmt.Sprintf("0x%x", block.NumberU64())
		context["blockHash"] = block.Hash().Hex()
	}

	if parent != nil {
		context["parentNumber"] = fmt.Sprintf("0x%x", parent.Number.Uint64())
		context["parentHash"] = parent.Hash().Hex()
	}

	return context
}

// FormatTransactionContext creates structured context for transaction-related errors.
func FormatTransactionContext(tx *types.Transaction, index int, from string) map[string]interface{} {
	context := make(map[string]interface{})

	context["txIndex"] = index
	if tx != nil {
		context["txHash"] = tx.Hash().Hex()
		context["txType"] = int(tx.Type())
		context["txNonce"] = tx.Nonce()
		context["txGas"] = tx.Gas()
		if from != "" {
			context["from"] = from
		}
		if tx.To() != nil {
			context["to"] = tx.To().Hex()
		}
		if tx.Value() != nil {
			context["value"] = fmt.Sprintf("0x%x", tx.Value())
		}
	}

	return context
}

// FormatGasContext creates structured context for gas-related errors.
func FormatGasContext(gasUsed, gasLimit uint64, baseFee, gasPrice *big.Int) map[string]interface{} {
	context := make(map[string]interface{})

	context["gasUsed"] = gasUsed
	context["gasLimit"] = gasLimit

	if baseFee != nil {
		context["baseFee"] = fmt.Sprintf("0x%x", baseFee)
	}
	if gasPrice != nil {
		context["gasPrice"] = fmt.Sprintf("0x%x", gasPrice)
	}

	return context
}
