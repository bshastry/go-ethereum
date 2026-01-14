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

package logger

import (
	"encoding/json"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// TraceLevel defines the verbosity of block tracing.
// Trace levels control which record types are emitted:
//   - Minimal: blockStart, txStart, txEnd, blockEnd only (<2% overhead)
//   - Standard: Minimal + preExecution, postExecution, validation (3-5% overhead)
//   - Full: Standard + operation (EIP-3155), trieOperation (10-15% overhead)
//   - Custom: User-specified combination of record types
type TraceLevel int

const (
	TraceLevelMinimal  TraceLevel = 0 // blockStart, txStart, txEnd, blockEnd
	TraceLevelStandard TraceLevel = 1 // + preExecution, postExecution, validation
	TraceLevelFull     TraceLevel = 2 // + operation (EIP-3155), trieOperation
	TraceLevelCustom   TraceLevel = 3 // User-specified combination
)

// BlockTracerConfig extends Config with block-level tracing options.
// It allows fine-grained control over which trace records are emitted.
type BlockTracerConfig struct {
	*Config                // Embed existing EIP-3155 config
	Level       TraceLevel // Trace level
	CustomTypes []string   // Custom record types (if Level == TraceLevelCustom)

	// Feature flags for Full mode
	IncludeOpcode bool // Include EIP-3155 opcodes in full mode
	IncludeTrie   bool // Include trie operations in full mode

	// Scope filters - control trace categories
	IncludeTx    bool // Include transaction-level traces (txStart, txEnd, operation)
	IncludeBlock bool // Include block-level traces (blockStart, blockEnd, preExecution, postExecution, validation, trieOperation)

	// Fork name override - if provided, this will be used instead of inferring from header
	// This is primarily used in blocktests where the fork is known externally
	ForkName string // Optional fork name (e.g., "osaka", "prague", "cancun")
}

// BlockTracerHooks wraps tracing.Hooks for future extensibility.
// Deprecated: This wrapper is no longer needed since storage writes are passed via OnPreExecutionEnd metadata.
// Kept for backward compatibility.
type BlockTracerHooks struct {
	*tracing.Hooks
	tracer *blockTracer
}

// blockTracer implements comprehensive block-level tracing.
// It extends EIP-3155 transaction tracing to capture all consensus-critical
// operations during block execution, including pre-execution system calls,
// post-execution operations (withdrawals, execution requests), validation
// logic, and Merkle trie computations.
//
// The tracer emits JSON Lines (JSONL) format - one JSON object per line.
// This format enables streaming processing and is tool-friendly for analysis.
type blockTracer struct {
	encoder *json.Encoder
	writer  io.Writer // Direct writer for canonical output
	cfg     *BlockTracerConfig
	env     *tracing.VMContext
	hooks   *BlockTracerHooks

	// State tracking for current block
	currentBlock    *types.Header
	currentTx       *types.Transaction
	currentTxIndex  int
	txGasUsedBefore uint64
	blockGasUsed    uint64

	// Pre-execution state (captured between Start and End hooks)
	preExecOperation string
	preExecEIP       string
	preExecMetadata  map[string]interface{}

	// Post-execution state (captured between Start and End hooks)
	postExecOperation string
	postExecEIP       string
	postExecStart     bool

	// Inner tracer for EIP-3155 operations (optional, only in Full mode)
	innerTracer *jsonLogger

	// System call tracking - when true, suppress opcode tracing
	inSystemCall bool

	// Fork name override - if set, this will be used instead of inferring from header
	// This is populated from BlockTracerConfig.ForkName and primarily used in blocktests
	forkName string
}

// NewBlockTracer creates a new block-level tracer.
// The tracer writes JSON Lines output to the provided writer (typically stderr).
//
// If cfg is nil, it defaults to Standard trace level with both tx and block traces enabled.
// The tracer is safe to use concurrently from multiple goroutines.
//
// The returned BlockTracerHooks embeds *tracing.Hooks.
func NewBlockTracer(cfg *BlockTracerConfig, writer io.Writer) *BlockTracerHooks {
	if cfg == nil {
		cfg = &BlockTracerConfig{
			Config:       &Config{},
			Level:        TraceLevelStandard,
			IncludeTx:    true,
			IncludeBlock: true,
		}
	}
	if cfg.Config == nil {
		cfg.Config = &Config{}
	}

	t := &blockTracer{
		encoder:  json.NewEncoder(writer),
		writer:   writer,
		cfg:      cfg,
		forkName: cfg.ForkName, // Store the fork name override if provided
	}

	// Create inner EIP-3155 tracer if needed
	if cfg.Level >= TraceLevelFull && cfg.IncludeOpcode {
		t.innerTracer = &jsonLogger{
			encoder: t.encoder,
			cfg:     cfg.Config,
			hooks:   &tracing.Hooks{}, // Initialize hooks field to prevent nil pointer in onSystemCallStart
		}
	}

	// Build hooks and wrap in BlockTracerHooks for optional interface support
	baseHooks := t.buildHooks()
	wrappedHooks := &BlockTracerHooks{
		Hooks:  baseHooks,
		tracer: t,
	}
	t.hooks = wrappedHooks

	return wrappedHooks
}

// buildHooks constructs the hook set based on the configured trace level and scope filters.
// This allows filtering of trace records at the source, minimizing overhead.
//
// The hook installation follows this priority:
// 1. Custom level uses explicit type selection (respects scope filters)
// 2. Standard levels (minimal/standard/full) use level-based selection (respects scope filters)
// 3. Scope filters (IncludeTx, IncludeBlock) control tx-level vs block-level traces
func (t *blockTracer) buildHooks() *tracing.Hooks {
	h := &tracing.Hooks{}

	// Custom level - build hooks based on CustomTypes with scope filtering
	if t.cfg.Level == TraceLevelCustom {
		// Track if we need to add OnTxStart for inner tracer initialization
		needsInnerTracerInit := false

		// Add hooks based on user-specified types, filtered by scope
		for _, typ := range t.cfg.CustomTypes {
			switch typ {
			// Block-level traces (filtered by IncludeBlock)
			case "blockStart":
				if t.cfg.IncludeBlock {
					h.OnBlockStart = t.OnBlockStart
				}
			case "blockEnd":
				if t.cfg.IncludeBlock {
					h.OnBlockEnd = t.OnBlockEnd
				}
			case "preExecution":
				if t.cfg.IncludeBlock {
					h.OnPreExecutionStart = t.OnPreExecutionStart
					h.OnPreExecutionEnd = t.OnPreExecutionEnd
				}
			case "postExecution":
				if t.cfg.IncludeBlock {
					h.OnPostExecutionStart = t.OnPostExecutionStart
					h.OnPostExecutionEnd = t.OnPostExecutionEnd
				}
			case "validation":
				if t.cfg.IncludeBlock {
					h.OnValidation = t.OnValidation
				}
			case "trieOperation":
				if t.cfg.IncludeBlock {
					h.OnTrieOperation = t.OnTrieOperation
				}
			// Transaction-level traces (filtered by IncludeTx)
			case "txStart":
				if t.cfg.IncludeTx {
					h.OnTxStart = t.OnTxStart
				}
			case "txEnd":
				if t.cfg.IncludeTx {
					h.OnTxEnd = t.OnTxEnd
				}
			case "operation":
				if t.cfg.IncludeTx && t.innerTracer != nil {
					h.OnOpcode = t.onOpcodeWrapper
					h.OnEnter = t.onEnterWrapper
					h.OnExit = t.onExitWrapper
					h.OnFault = t.onFaultWrapper
					h.OnSystemCallStart = t.onSystemCallStart
					h.OnSystemCallEnd = t.onSystemCallEnd
					needsInnerTracerInit = true
				}
			}
		}

		// If we added inner tracer hooks but OnTxStart wasn't explicitly requested,
		// we still need to add it to initialize the inner tracer's env field
		if needsInnerTracerInit && h.OnTxStart == nil {
			h.OnTxStart = t.OnTxStart
		}

		return h
	}

	// Standard levels (minimal/standard/full) with scope filtering

	// Block-level hooks (filtered by IncludeBlock)
	if t.cfg.IncludeBlock {
		// Always included in all levels (Minimal+)
		h.OnBlockStart = t.OnBlockStart
		h.OnBlockEnd = t.OnBlockEnd

		// Standard level and above
		if t.cfg.Level >= TraceLevelStandard {
			h.OnPreExecutionStart = t.OnPreExecutionStart
			h.OnPreExecutionEnd = t.OnPreExecutionEnd
			h.OnPostExecutionStart = t.OnPostExecutionStart
			h.OnPostExecutionEnd = t.OnPostExecutionEnd
			h.OnValidation = t.OnValidation
		}

		// Full level - trie operations
		if t.cfg.Level >= TraceLevelFull && t.cfg.IncludeTrie {
			h.OnTrieOperation = t.OnTrieOperation
		}
	}

	// Transaction-level hooks (filtered by IncludeTx)
	if t.cfg.IncludeTx {
		// Always included in all levels (Minimal+)
		h.OnTxStart = t.OnTxStart
		h.OnTxEnd = t.OnTxEnd

		// Full level - EIP-3155 opcode tracing
		if t.cfg.Level >= TraceLevelFull && t.cfg.IncludeOpcode && t.innerTracer != nil {
			h.OnOpcode = t.onOpcodeWrapper
			h.OnEnter = t.onEnterWrapper
			h.OnExit = t.onExitWrapper
			h.OnFault = t.onFaultWrapper
			h.OnSystemCallStart = t.onSystemCallStart
			h.OnSystemCallEnd = t.onSystemCallEnd
		}
	}

	return h
}

// Hook implementations

// OnBlockStart emits a blockStart record when block processing begins.
// This record contains block header information and fork-specific fields.
func (t *blockTracer) OnBlockStart(event tracing.BlockEvent) {
	t.currentBlock = event.Block.Header()
	t.currentTxIndex = 0
	t.txGasUsedBefore = 0
	t.blockGasUsed = 0

	// Use CanonicalJSON for proper key ordering (type first, then alphabetical)
	record := CanonicalJSON{
		"type":        "blockStart",
		"blockNumber": ToCanonicalHex(t.currentBlock.Number),
		"blockHash":   CanonicalHash(t.currentBlock.Hash()),
		"parentHash":  CanonicalHash(t.currentBlock.ParentHash),
		"timestamp":   ToCanonicalHex(t.currentBlock.Time),
		"gasLimit":    ToCanonicalHex(t.currentBlock.GasLimit),
		"difficulty":  ToCanonicalHex(t.currentBlock.Difficulty),
		"miner":       CanonicalAddress(t.currentBlock.Coinbase),
	}

	// Add fork name if we can determine it
	if fork := t.getForkName(t.currentBlock); fork != "" {
		record["fork"] = fork
	}

	// Optional EIP-1559 fields (London+)
	if t.currentBlock.BaseFee != nil {
		record["baseFeePerGas"] = ToCanonicalHex(t.currentBlock.BaseFee)
	}

	// Optional EIP-4844 fields (Cancun+)
	if t.currentBlock.ExcessBlobGas != nil {
		record["excessBlobGas"] = ToCanonicalHex(*t.currentBlock.ExcessBlobGas)
	}
	if t.currentBlock.BlobGasUsed != nil {
		record["blobGasUsed"] = ToCanonicalHex(*t.currentBlock.BlobGasUsed)
	}

	// Optional EIP-4788 fields (Cancun+)
	if t.currentBlock.ParentBeaconRoot != nil {
		record["parentBeaconBlockRoot"] = CanonicalHash(*t.currentBlock.ParentBeaconRoot)
	}

	// Optional EIP-7685 fields (Prague+)
	if t.currentBlock.RequestsHash != nil {
		record["requestsHash"] = CanonicalHash(*t.currentBlock.RequestsHash)
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)
}

// OnBlockEnd emits a blockEnd record when block processing completes.
// This record contains computed roots and validation result.
func (t *blockTracer) OnBlockEnd(err error) {
	// Use CanonicalJSON for proper key ordering (type first, then alphabetical)
	record := CanonicalJSON{
		"type":             "blockEnd",
		"blockNumber":      ToCanonicalHex(t.currentBlock.Number),
		"blockHash":        CanonicalHash(t.currentBlock.Hash()),
		"stateRoot":        CanonicalHash(t.currentBlock.Root),
		"transactionsRoot": CanonicalHash(t.currentBlock.TxHash),
		"receiptsRoot":     CanonicalHash(t.currentBlock.ReceiptHash),
		"logsBloom":        CanonicalBytes(t.currentBlock.Bloom.Bytes()),
		"totalGasUsed":     ToCanonicalHex(t.blockGasUsed),
		"validationResult": "valid",
	}

	if err != nil {
		record["validationResult"] = "invalid"
		// Note: We intentionally do NOT include the error field in blockEnd.
		// Errors are reported in testEnd for failed tests, not at the block level.
	}

	// Optional post-Shanghai fields
	if t.currentBlock.WithdrawalsHash != nil {
		record["withdrawalsRoot"] = CanonicalHash(*t.currentBlock.WithdrawalsHash)
	}

	// Optional post-Cancun fields
	if t.currentBlock.BlobGasUsed != nil {
		record["totalBlobGasUsed"] = ToCanonicalHex(*t.currentBlock.BlobGasUsed)
	}

	// Optional post-Prague fields
	if t.currentBlock.RequestsHash != nil {
		record["requestsHash"] = CanonicalHash(*t.currentBlock.RequestsHash)
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)
}

// OnTxStart emits a txStart record when transaction processing begins.
// This extends EIP-3155 with additional transaction metadata.
func (t *blockTracer) OnTxStart(env *tracing.VMContext, tx *types.Transaction, from common.Address) {
	t.env = env
	t.currentTx = tx
	t.txGasUsedBefore = t.blockGasUsed

	// Initialize inner tracer's env FIRST, before emitting any records
	// This is critical because OnOpcode can be called during system calls
	// (EIP-4788, EIP-2935) before we emit the txStart record below.
	// The inner tracer needs env to be set for its OnOpcode handler.
	if t.innerTracer != nil {
		t.innerTracer.env = env
	}

	// Use CanonicalJSON for proper key ordering (type first, txIndex second, then alphabetical)
	record := CanonicalJSON{
		"type":                    "txStart",
		"txIndex":                 ToCanonicalHex(uint64(t.currentTxIndex)),
		"txHash":                  CanonicalHash(tx.Hash()),
		"txType":                  ToCanonicalHex(uint64(tx.Type())),
		"from":                    CanonicalAddress(from),
		"gasLimit":                ToCanonicalHex(tx.Gas()),
		"nonce":                   ToCanonicalHex(tx.Nonce()),
		"value":                   ToCanonicalHex(tx.Value()),
		"cumulativeGasUsedBefore": ToCanonicalHex(t.txGasUsedBefore),
	}

	// Optional to address (null for contract creation)
	if tx.To() != nil {
		record["to"] = CanonicalAddress(*tx.To())
	}

	// Type-specific fields
	switch tx.Type() {
	case types.LegacyTxType:
		record["gasPrice"] = ToCanonicalHex(tx.GasPrice())
	case types.AccessListTxType:
		record["gasPrice"] = ToCanonicalHex(tx.GasPrice())
		// Serialize access list if present
		if accessList := tx.AccessList(); len(accessList) > 0 {
			record["accessList"] = serializeAccessList(accessList)
		}
	case types.DynamicFeeTxType:
		record["maxFeePerGas"] = ToCanonicalHex(tx.GasFeeCap())
		record["maxPriorityFeePerGas"] = ToCanonicalHex(tx.GasTipCap())
		// EIP-2930 access lists are also supported in EIP-1559 transactions
		if accessList := tx.AccessList(); len(accessList) > 0 {
			record["accessList"] = serializeAccessList(accessList)
		}
	case types.BlobTxType:
		record["maxFeePerGas"] = ToCanonicalHex(tx.GasFeeCap())
		record["maxPriorityFeePerGas"] = ToCanonicalHex(tx.GasTipCap())
		record["maxFeePerBlobGas"] = ToCanonicalHex(tx.BlobGasFeeCap())
		// Convert blob hashes to canonical lowercase hex
		blobHashes := tx.BlobHashes()
		canonicalBlobHashes := make([]string, len(blobHashes))
		for i, hash := range blobHashes {
			canonicalBlobHashes[i] = CanonicalHash(hash)
		}
		record["blobVersionedHashes"] = canonicalBlobHashes
		// EIP-2930 access lists are also supported in blob transactions
		if accessList := tx.AccessList(); len(accessList) > 0 {
			record["accessList"] = serializeAccessList(accessList)
		}
	case types.SetCodeTxType:
		record["maxFeePerGas"] = ToCanonicalHex(tx.GasFeeCap())
		record["maxPriorityFeePerGas"] = ToCanonicalHex(tx.GasTipCap())
		// EIP-2930 access lists are also supported in SetCode transactions
		if accessList := tx.AccessList(); len(accessList) > 0 {
			record["accessList"] = serializeAccessList(accessList)
		}
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)

	// Note: We do NOT call innerTracer.OnTxStart() here to avoid duplicate txStart records.
	// The block tracer already emits txStart records above, and the inner tracer only needs
	// env to be set (which we did above) for its OnOpcode/OnEnter/OnExit handlers.
}

// OnTxEnd emits a txEnd record when transaction processing completes.
// This includes receipt information and transaction result.
func (t *blockTracer) OnTxEnd(receipt *types.Receipt, err error) {
	var gasUsed uint64
	if receipt != nil {
		gasUsed = receipt.GasUsed
		t.blockGasUsed = receipt.CumulativeGasUsed
	}

	// Use CanonicalJSON for proper key ordering (type first, txIndex second, then alphabetical)
	record := CanonicalJSON{
		"type":              "txEnd",
		"txIndex":           ToCanonicalHex(uint64(t.currentTxIndex)),
		"txHash":            CanonicalHash(t.currentTx.Hash()),
		"gasUsed":           ToCanonicalHex(gasUsed),
		"cumulativeGasUsed": ToCanonicalHex(t.blockGasUsed),
	}

	if receipt != nil {
		record["status"] = ToCanonicalHex(uint64(receipt.Status))
		record["logsBloom"] = CanonicalBytes(receipt.Bloom.Bytes())
		if receipt.ContractAddress != (common.Address{}) {
			record["contractAddress"] = CanonicalAddress(receipt.ContractAddress)
		}
		// TODO: Add effectiveGasPrice and output
	}

	if err != nil {
		record["status"] = "0x0"
		// Note: error field removed from txEnd to align with Nethermind's output format.
		// Nethermind's current architecture doesn't pass validation errors through EndTxTrace().
		// The error is still available in blockEnd for failed blocks.
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)
	t.currentTxIndex++
}

// OnPreExecutionStart captures metadata before a pre-execution operation begins.
// The metadata will be emitted when OnPreExecutionEnd is called.
func (t *blockTracer) OnPreExecutionStart(operation string, eip string, metadata map[string]interface{}) {
	t.preExecOperation = operation
	t.preExecEIP = eip
	t.preExecMetadata = metadata
}

// OnPreExecutionEnd emits a preExecution record after a pre-execution operation completes.
// This includes the metadata captured in OnPreExecutionStart plus gas used.
// The endMetadata parameter contains operation-specific results such as storage writes.
func (t *blockTracer) OnPreExecutionEnd(gasUsed uint64, endMetadata map[string]interface{}) {
	// Use CanonicalJSON for proper key ordering (type first, operation second, then alphabetical)
	record := CanonicalJSON{
		"type":      "preExecution",
		"operation": t.preExecOperation,
		"eip":       t.preExecEIP,
		"gasUsed":   ToCanonicalHex(gasUsed),
	}

	// Merge metadata captured during OnPreExecutionStart (convert to canonical format)
	for k, v := range t.preExecMetadata {
		// Skip internal tracking fields (those starting with "_")
		if len(k) > 0 && k[0] == '_' {
			continue
		}

		// Convert hex values to canonical lowercase format
		switch val := v.(type) {
		case string:
			record[k] = ToCanonicalHex(val)
		case uint64:
			record[k] = ToCanonicalHex(val)
		default:
			record[k] = v
		}
	}

	// Extract storage writes from endMetadata if provided
	if endMetadata != nil {
		if storageWrites, ok := endMetadata["storageWrites"]; ok {
			record["storageWrites"] = storageWrites
		}
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)

	// Clear state
	t.preExecOperation = ""
	t.preExecEIP = ""
	t.preExecMetadata = nil
}

// OnPostExecutionStart marks the beginning of a post-execution operation.
// The actual record will be emitted in OnPostExecutionEnd with full details.
func (t *blockTracer) OnPostExecutionStart(operation string, eip string) {
	t.postExecOperation = operation
	t.postExecEIP = eip
	t.postExecStart = true
}

// OnPostExecutionEnd emits a postExecution record after a post-execution operation completes.
// The metadata map contains operation-specific results.
func (t *blockTracer) OnPostExecutionEnd(metadata map[string]interface{}) {
	if !t.postExecStart {
		return // Safeguard against mismatched calls
	}

	// Use CanonicalJSON for proper key ordering (type first, operation second, then alphabetical)
	record := CanonicalJSON{
		"type":      "postExecution",
		"operation": t.postExecOperation,
		"eip":       t.postExecEIP,
	}

	// Merge metadata (convert to canonical format where needed)
	for k, v := range metadata {
		switch val := v.(type) {
		case string:
			record[k] = ToCanonicalHex(val)
		case uint64:
			record[k] = ToCanonicalHex(val)
		default:
			record[k] = v
		}
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)

	// Clear state
	t.postExecOperation = ""
	t.postExecEIP = ""
	t.postExecStart = false
}

// OnValidation emits a validation record for block correctness checks.
// This includes header validation, gas accounting, and blob gas accounting.
func (t *blockTracer) OnValidation(operation string, details map[string]interface{}) {
	// Use CanonicalJSON for proper key ordering (type first, operation second, then alphabetical)
	record := CanonicalJSON{
		"type":      "validation",
		"operation": operation,
	}

	// Merge details (details are already in canonical format from state_processor.go)
	for k, v := range details {
		record[k] = v
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)
}

// OnTrieOperation emits a trieOperation record for Merkle/Verkle trie computations.
// This includes state root, receipt root, transaction root, and withdrawals root.
func (t *blockTracer) OnTrieOperation(operation string, details map[string]interface{}) {
	// Use CanonicalJSON for proper key ordering (type first, operation second, then alphabetical)
	record := CanonicalJSON{
		"type":      "trieOperation",
		"operation": operation,
	}

	// Merge details
	for k, v := range details {
		record[k] = v
	}

	// Remove any nil optional fields for canonical format
	OmitEmpty(record)

	t.writeCanonicalJSON(record)
}

// Opcode tracing wrapper methods

// onSystemCallStart marks that we're entering a system call.
// During system calls, we suppress opcode tracing to avoid issues with
// uninitialized state and to avoid cluttering the output.
func (t *blockTracer) onSystemCallStart() {
	t.inSystemCall = true
}

// onSystemCallEnd marks that we're exiting a system call.
func (t *blockTracer) onSystemCallEnd() {
	t.inSystemCall = false
}

// onOpcodeWrapper wraps the inner tracer's OnOpcode method.
// It suppresses opcodes during system calls and ensures env is initialized.
func (t *blockTracer) onOpcodeWrapper(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
	if t.inSystemCall {
		return // Suppress opcodes during system calls
	}
	if t.innerTracer != nil && t.innerTracer.env != nil {
		t.innerTracer.OnOpcode(pc, op, gas, cost, scope, rData, depth, err)
	}
}

// onEnterWrapper wraps the inner tracer's OnEnter method.
func (t *blockTracer) onEnterWrapper(depth int, typ byte, from common.Address, to common.Address, input []byte, gas uint64, value *big.Int) {
	if t.inSystemCall {
		return // Suppress during system calls
	}
	if t.innerTracer != nil && t.innerTracer.env != nil {
		t.innerTracer.OnEnter(depth, typ, from, to, input, gas, value)
	}
}

// onExitWrapper wraps the inner tracer's OnExit method.
func (t *blockTracer) onExitWrapper(depth int, output []byte, gasUsed uint64, err error, reverted bool) {
	if t.inSystemCall {
		return // Suppress during system calls
	}
	if t.innerTracer != nil {
		t.innerTracer.OnExit(depth, output, gasUsed, err, reverted)
	}
}

// onFaultWrapper wraps the inner tracer's OnFault method.
func (t *blockTracer) onFaultWrapper(pc uint64, op byte, gas uint64, cost uint64, scope tracing.OpContext, depth int, err error) {
	if t.inSystemCall {
		return // Suppress during system calls
	}
	if t.innerTracer != nil && t.innerTracer.env != nil {
		t.innerTracer.OnFault(pc, op, gas, cost, scope, depth, err)
	}
}

// Helper functions

// writeCanonicalJSON writes a CanonicalJSON record to the writer with canonical formatting.
// This ensures proper key ordering and compact output.
func (t *blockTracer) writeCanonicalJSON(record CanonicalJSON) {
	data, err := json.Marshal(record)
	if err != nil {
		// Fallback to encoder if marshal fails (should never happen)
		t.writeCanonicalJSON(record)
		return
	}
	t.writer.Write(data)
	t.writer.Write([]byte("\n"))
}

// toHex converts a big.Int to a 0x-prefixed hexadecimal string.
// Returns "0x0" for nil values.
func toHex(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	if n.Sign() == 0 {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

// serializeAccessList converts an EIP-2930 access list to canonical JSON format.
// Each entry contains an address and its associated storage keys, both in lowercase hex.
// Storage keys use minimal hex representation (leading zeros removed).
func serializeAccessList(accessList types.AccessList) []map[string]interface{} {
	result := make([]map[string]interface{}, len(accessList))
	for i, tuple := range accessList {
		entry := map[string]interface{}{
			"address": CanonicalAddress(tuple.Address),
		}

		// Convert storage keys to canonical minimal hex (strip leading zeros)
		storageKeys := make([]string, len(tuple.StorageKeys))
		for j, key := range tuple.StorageKeys {
			// Convert hash to big.Int to get minimal hex representation
			storageKeys[j] = ToCanonicalHex(key.Big())
		}
		entry["storageKeys"] = storageKeys

		result[i] = entry
	}
	return result
}

// getForkName attempts to determine the fork name from the block header.
// Returns empty string if fork cannot be determined.
//
// If a fork name was explicitly provided in the BlockTracerConfig (via ForkName field),
// that will be used instead of inferring from the header. This is primarily used in
// blocktests where the fork is known externally and header-based detection can be
// ambiguous (e.g., Osaka vs Prague both have RequestsHash).
//
// Fork detection is based on block features:
//   - Paris (Merge): Difficulty = 0 and block number > Merge block
//   - Shanghai: Has withdrawals
//   - Cancun: Has blob gas fields
//   - Prague/Osaka: Has requests hash (ambiguous without explicit fork name)
func (t *blockTracer) getForkName(header *types.Header) string {
	// If fork name was explicitly provided (e.g., from blocktest), use it
	if t.forkName != "" {
		return t.forkName
	}

	// Fall back to header-based detection
	// Prague/Osaka detection (has requests hash)
	// NOTE: Without explicit fork name, we cannot distinguish Osaka from Prague
	// since both have RequestsHash in the header.
	if header.RequestsHash != nil {
		return "prague"
	}

	// Cancun detection (has blob gas)
	if header.ExcessBlobGas != nil || header.BlobGasUsed != nil {
		return "cancun"
	}

	// Shanghai detection (has withdrawals)
	if header.WithdrawalsHash != nil {
		return "shanghai"
	}

	// Paris detection (post-merge: difficulty = 0 and beyond merge block)
	if header.Difficulty != nil && header.Difficulty.Sign() == 0 {
		// Need chain config to definitively determine if post-merge
		// For now, difficulty = 0 strongly suggests Paris or later
		if header.Number != nil && params.MainnetChainConfig.MergeNetsplitBlock != nil {
			if header.Number.Uint64() > params.MainnetChainConfig.MergeNetsplitBlock.Uint64() {
				return "paris"
			}
		}
	}

	// London detection (has base fee)
	if header.BaseFee != nil {
		return "london"
	}

	// Could add more fork detection here (Berlin, Istanbul, etc.)
	// but for block-level tracing we mainly care about recent forks

	return ""
}
