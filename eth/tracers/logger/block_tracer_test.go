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
	"bytes"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
)

// TestBlockTracerMinimal verifies that minimal trace level emits only basic records.
func TestBlockTracerMinimal(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelMinimal,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate minimal block execution (no transactions)
	block := types.NewBlock(&types.Header{
		Number:     big.NewInt(1000000),
		GasLimit:   30000000,
		Difficulty: big.NewInt(0),
		Time:       1234567890,
		Coinbase:   common.HexToAddress("0x0000000000000000000000000000000000000001"),
	}, &types.Body{}, nil, trie.NewStackTrie(nil))

	event := tracing.BlockEvent{Block: block}
	hooks.OnBlockStart(event)
	hooks.OnBlockEnd(nil)

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	// Verify we got exactly 2 records
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Check blockStart record
	if records[0]["type"] != "blockStart" {
		t.Errorf("expected type=blockStart, got %s", records[0]["type"])
	}
	if records[0]["blockNumber"] != "0xf4240" { // 1000000 in hex
		t.Errorf("expected blockNumber=0xf4240, got %s", records[0]["blockNumber"])
	}

	// Check blockEnd record
	if records[1]["type"] != "blockEnd" {
		t.Errorf("expected type=blockEnd, got %s", records[1]["type"])
	}
	if records[1]["validationResult"] != "valid" {
		t.Errorf("expected validationResult=valid, got %s", records[1]["validationResult"])
	}
}

// TestBlockTracerMinimalWithTransaction tests minimal tracing with a transaction.
func TestBlockTracerMinimalWithTransaction(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelMinimal,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Create block
	block := types.NewBlock(&types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30000000,
		Time:     1234567890,
		Coinbase: common.HexToAddress("0x1"),
	}, &types.Body{}, nil, trie.NewStackTrie(nil))

	// Create transaction
	tx := types.NewTx(&types.LegacyTx{
		Nonce:    5,
		GasPrice: big.NewInt(1000000000),
		Gas:      21000,
		To:       &common.Address{0x2},
		Value:    big.NewInt(1000000000000000000),
	})

	// Create receipt
	receipt := &types.Receipt{
		Status:            types.ReceiptStatusSuccessful,
		GasUsed:           21000,
		CumulativeGasUsed: 21000,
	}

	// Simulate execution
	event := tracing.BlockEvent{Block: block}
	hooks.OnBlockStart(event)
	hooks.OnTxStart(&tracing.VMContext{}, tx, common.HexToAddress("0x3"))
	hooks.OnTxEnd(receipt, nil)
	hooks.OnBlockEnd(nil)

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	// Verify we got 4 records: blockStart, txStart, txEnd, blockEnd
	if len(records) != 4 {
		t.Fatalf("expected 4 records, got %d", len(records))
	}

	expectedTypes := []string{"blockStart", "txStart", "txEnd", "blockEnd"}
	for i, expected := range expectedTypes {
		if records[i]["type"] != expected {
			t.Errorf("record %d: expected type=%s, got %s", i, expected, records[i]["type"])
		}
	}

	// Verify txStart details
	txStart := records[1]
	if txStart["txIndex"] != "0x0" {
		t.Errorf("expected txIndex=0x0, got %s", txStart["txIndex"])
	}
	if txStart["gasLimit"] != "0x5208" { // 21000 in hex
		t.Errorf("expected gasLimit=0x5208, got %s", txStart["gasLimit"])
	}

	// Verify txEnd details
	txEnd := records[2]
	if txEnd["status"] != "0x1" {
		t.Errorf("expected status=0x1, got %s", txEnd["status"])
	}
	if txEnd["gasUsed"] != "0x5208" {
		t.Errorf("expected gasUsed=0x5208, got %s", txEnd["gasUsed"])
	}
}

// TestBlockTracerStandard verifies that standard trace level includes pre/post execution.
func TestBlockTracerStandard(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Verify hooks are set
	if hooks.OnPreExecutionStart == nil {
		t.Error("expected OnPreExecutionStart hook to be set")
	}
	if hooks.OnPreExecutionEnd == nil {
		t.Error("expected OnPreExecutionEnd hook to be set")
	}
	if hooks.OnPostExecutionStart == nil {
		t.Error("expected OnPostExecutionStart hook to be set")
	}
	if hooks.OnPostExecutionEnd == nil {
		t.Error("expected OnPostExecutionEnd hook to be set")
	}
	if hooks.OnValidation == nil {
		t.Error("expected OnValidation hook to be set")
	}

	// Verify opcode hook is NOT set (only in Full mode)
	if hooks.OnOpcode != nil {
		t.Error("expected OnOpcode hook to be nil in Standard mode")
	}
	if hooks.OnTrieOperation != nil {
		t.Error("expected OnTrieOperation hook to be nil in Standard mode (requires Full + IncludeTrie)")
	}
}

// TestBlockTracerFull verifies that full trace level includes opcodes and trie ops.
func TestBlockTracerFull(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:        &Config{},
		Level:         TraceLevelFull,
		IncludeTx:     true,
		IncludeBlock:  true,
		IncludeOpcode: true,
		IncludeTrie:   true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Verify opcode hooks are set
	if hooks.OnOpcode == nil {
		t.Error("expected OnOpcode hook to be set in Full mode with IncludeOpcode")
	}
	if hooks.OnEnter == nil {
		t.Error("expected OnEnter hook to be set in Full mode with IncludeOpcode")
	}
	if hooks.OnExit == nil {
		t.Error("expected OnExit hook to be set in Full mode with IncludeOpcode")
	}
	if hooks.OnFault == nil {
		t.Error("expected OnFault hook to be set in Full mode with IncludeOpcode")
	}

	// Verify trie hook is set
	if hooks.OnTrieOperation == nil {
		t.Error("expected OnTrieOperation hook to be set in Full mode with IncludeTrie")
	}

	// Verify standard hooks are still set
	if hooks.OnPreExecutionStart == nil {
		t.Error("expected OnPreExecutionStart hook to be set")
	}
	if hooks.OnValidation == nil {
		t.Error("expected OnValidation hook to be set")
	}
}

// TestBlockTracerCustom verifies that custom trace level respects user-specified types.
func TestBlockTracerCustom(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelCustom,
		IncludeTx:    true,
		IncludeBlock: true,
		CustomTypes:  []string{"blockStart", "preExecution", "blockEnd"},
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Verify specified hooks are set
	if hooks.OnBlockStart == nil {
		t.Error("expected OnBlockStart hook to be set")
	}
	if hooks.OnBlockEnd == nil {
		t.Error("expected OnBlockEnd hook to be set")
	}
	if hooks.OnPreExecutionStart == nil {
		t.Error("expected OnPreExecutionStart hook to be set")
	}
	if hooks.OnPreExecutionEnd == nil {
		t.Error("expected OnPreExecutionEnd hook to be set")
	}

	// Verify unspecified hooks are NOT set
	if hooks.OnTxStart != nil {
		t.Error("expected OnTxStart hook to be nil (not in custom types)")
	}
	if hooks.OnTxEnd != nil {
		t.Error("expected OnTxEnd hook to be nil (not in custom types)")
	}
	if hooks.OnPostExecutionStart != nil {
		t.Error("expected OnPostExecutionStart hook to be nil (not in custom types)")
	}
	if hooks.OnValidation != nil {
		t.Error("expected OnValidation hook to be nil (not in custom types)")
	}
}

// TestPreExecutionRecords tests pre-execution record emission.
func TestPreExecutionRecords(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate EIP-4788 beacon root storage
	metadata := map[string]interface{}{
		"timestamp":             "0x12345678",
		"parentBeaconBlockRoot": "0x" + strings.Repeat("aa", 32),
		"contractAddress":       "0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02",
		"ringBuffer": map[string]interface{}{
			"index":         "0x4d2",
			"timestampSlot": "0x09a4",
			"rootSlot":      "0x09a5",
		},
	}

	hooks.OnPreExecutionStart("beaconRootStorage", "4788", metadata)
	hooks.OnPreExecutionEnd(100000, nil)

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record["type"] != "preExecution" {
		t.Errorf("expected type=preExecution, got %s", record["type"])
	}
	if record["operation"] != "beaconRootStorage" {
		t.Errorf("expected operation=beaconRootStorage, got %s", record["operation"])
	}
	if record["eip"] != "4788" {
		t.Errorf("expected eip=4788, got %s", record["eip"])
	}
	if record["gasUsed"] != "0x186a0" { // 100000 in hex
		t.Errorf("expected gasUsed=0x186a0, got %s", record["gasUsed"])
	}

	// Verify metadata was merged
	if record["timestamp"] != "0x12345678" {
		t.Errorf("expected timestamp from metadata, got %s", record["timestamp"])
	}
}

// TestPostExecutionRecords tests post-execution record emission.
func TestPostExecutionRecords(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate withdrawals
	hooks.OnPostExecutionStart("withdrawals", "4895")
	hooks.OnPostExecutionEnd(map[string]interface{}{
		"totalWithdrawn":   "0x6f05b59d3b20000",
		"accountsCreated":  "0x0",
		"withdrawalCount":  3,
	})

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record["type"] != "postExecution" {
		t.Errorf("expected type=postExecution, got %s", record["type"])
	}
	if record["operation"] != "withdrawals" {
		t.Errorf("expected operation=withdrawals, got %s", record["operation"])
	}
	if record["eip"] != "4895" {
		t.Errorf("expected eip=4895, got %s", record["eip"])
	}

	// Verify metadata
	withdrawalCount := record["withdrawalCount"]
	if withdrawalCount.(float64) != 3 {
		t.Errorf("expected withdrawalCount=3, got %v", withdrawalCount)
	}
}

// TestValidationRecords tests validation record emission.
func TestValidationRecords(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate gas accounting validation
	hooks.OnValidation("gasAccounting", map[string]interface{}{
		"blockGasLimit": "0x1c9c380",
		"totalGasUsed":  "0x17990",
		"valid":         true,
	})

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record["type"] != "validation" {
		t.Errorf("expected type=validation, got %s", record["type"])
	}
	if record["operation"] != "gasAccounting" {
		t.Errorf("expected operation=gasAccounting, got %s", record["operation"])
	}

	// Verify validation result
	if record["valid"] != true {
		t.Errorf("expected valid=true, got %v", record["valid"])
	}
}

// TestTrieOperationRecords tests trie operation record emission.
func TestTrieOperationRecords(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelFull,
		IncludeTx:    true,
		IncludeBlock: true,
		IncludeTrie:  true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate state root calculation
	hooks.OnTrieOperation("stateRoot", map[string]interface{}{
		"trieType":          "merklePatricia",
		"finalStateRoot":    "0x" + strings.Repeat("11", 32),
		"expectedStateRoot": "0x" + strings.Repeat("11", 32),
		"valid":             true,
	})

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]
	if record["type"] != "trieOperation" {
		t.Errorf("expected type=trieOperation, got %s", record["type"])
	}
	if record["operation"] != "stateRoot" {
		t.Errorf("expected operation=stateRoot, got %s", record["operation"])
	}
	if record["trieType"] != "merklePatricia" {
		t.Errorf("expected trieType=merklePatricia, got %s", record["trieType"])
	}
}

// TestToHex verifies hexadecimal conversion helper.
func TestToHex(t *testing.T) {
	tests := []struct {
		input    *big.Int
		expected string
	}{
		{nil, "0x0"},
		{big.NewInt(0), "0x0"},
		{big.NewInt(1), "0x1"},
		{big.NewInt(255), "0xff"},
		{big.NewInt(256), "0x100"},
		{big.NewInt(1000000), "0xf4240"},
		{big.NewInt(21000), "0x5208"},
	}

	for _, tt := range tests {
		result := toHex(tt.input)
		if result != tt.expected {
			t.Errorf("toHex(%v) = %s, want %s", tt.input, result, tt.expected)
		}
	}
}

// TestGetForkName verifies fork detection logic.
func TestGetForkName(t *testing.T) {
	tests := []struct {
		name     string
		header   *types.Header
		expected string
	}{
		{
			name: "Prague (has requests hash)",
			header: &types.Header{
				RequestsHash: &common.Hash{0x1},
			},
			expected: "prague",
		},
		{
			name: "Cancun (has blob gas)",
			header: &types.Header{
				ExcessBlobGas: new(uint64),
			},
			expected: "cancun",
		},
		{
			name: "Shanghai (has withdrawals)",
			header: &types.Header{
				WithdrawalsHash: &common.Hash{0x1},
			},
			expected: "shanghai",
		},
		{
			name: "London (has base fee)",
			header: &types.Header{
				BaseFee: big.NewInt(1000000000),
			},
			expected: "london",
		},
		{
			name: "Pre-London",
			header: &types.Header{
				Difficulty: big.NewInt(1000),
			},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a tracer without fork name override to test header-based detection
			tracer := &blockTracer{
				forkName: "", // Empty to ensure header-based detection is used
			}
			result := tracer.getForkName(tt.header)
			if result != tt.expected {
				t.Errorf("getForkName() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// TestGetForkNameWithOverride verifies that explicit fork name overrides header-based detection.
func TestGetForkNameWithOverride(t *testing.T) {
	tests := []struct {
		name         string
		header       *types.Header
		forkOverride string
		expected     string
	}{
		{
			name: "Osaka override (has requests hash but should be osaka, not prague)",
			header: &types.Header{
				RequestsHash: &common.Hash{0x1},
			},
			forkOverride: "osaka",
			expected:     "osaka",
		},
		{
			name: "Prague override (explicit prague)",
			header: &types.Header{
				RequestsHash: &common.Hash{0x1},
			},
			forkOverride: "prague",
			expected:     "prague",
		},
		{
			name: "No override falls back to header detection",
			header: &types.Header{
				RequestsHash: &common.Hash{0x1},
			},
			forkOverride: "",
			expected:     "prague",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer := &blockTracer{
				forkName: tt.forkOverride,
			}
			result := tracer.getForkName(tt.header)
			if result != tt.expected {
				t.Errorf("getForkName() with override %q = %s, want %s", tt.forkOverride, result, tt.expected)
			}
		})
	}
}

// TestBlockEndWithError verifies error handling in blockEnd record.
func TestBlockEndWithError(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelMinimal,
		IncludeTx:    true,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, &types.Body{}, nil, trie.NewStackTrie(nil))

	event := tracing.BlockEvent{Block: block}
	hooks.OnBlockStart(event)
	hooks.OnBlockEnd(errors.New("invalid receipts root"))

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	blockEnd := records[1]
	if blockEnd["validationResult"] != "invalid" {
		t.Errorf("expected validationResult=invalid, got %s", blockEnd["validationResult"])
	}
	if blockEnd["error"] == nil {
		t.Error("expected error field to be present")
	}
}

// TestPreExecutionWithStorageWrites tests preExecution with ringBuffer and storageWrites.
// This verifies the enhanced preExecution traces that match Nethermind's detailed format.
func TestPreExecutionWithStorageWrites(t *testing.T) {
	var buf bytes.Buffer
	cfg := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeBlock: true,
	}
	hooks := NewBlockTracer(cfg, &buf)

	// Simulate EIP-4788 beacon root storage with ring buffer
	metadata := map[string]interface{}{
		"timestamp":             "0x3e8", // 1000 in decimal
		"parentBeaconBlockRoot": "0x6c31fc15422ebad28aaf9089c306702f67540b53c7eea8b7d2941044b027100f",
		"contractAddress":       "0x000f3df6d732807ef1319fb7b8bb8522d0beac02",
		"ringBuffer": map[string]interface{}{
			"index":         "0x3e8",  // timestamp % 8191 = 1000
			"timestampSlot": "0x7d0",  // 1000 * 2 = 2000
			"rootSlot":      "0x7d1",  // 2000 + 1 = 2001
		},
		// Internal tracking fields (should be filtered out in output)
		"_oldTimestamp": "0x0000000000000000000000000000000000000000000000000000000000000000",
		"_oldRoot":      "0x0000000000000000000000000000000000000000000000000000000000000000",
	}

	hooks.OnPreExecutionStart("beaconRootStorage", "4788", metadata)

	// Simulate storage writes metadata (new approach via OnPreExecutionEnd parameter)
	endMetadata := map[string]interface{}{
		"storageWrites": []map[string]interface{}{
			{
				"slot":     "0x7d0",
				"oldValue": "0x0",
				"newValue": "0x3e8", // timestamp value 1000
			},
			{
				"slot":     "0x7d1",
				"oldValue": "0x0",
				"newValue": "0x6c31fc15422ebad28aaf9089c306702f67540b53c7eea8b7d2941044b027100f",
			},
		},
	}

	hooks.OnPreExecutionEnd(100000, endMetadata)

	// Parse output
	lines := bytes.Split(buf.Bytes(), []byte("\n"))
	records := parseRecords(t, lines)

	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}

	record := records[0]

	// Verify basic fields
	if record["type"] != "preExecution" {
		t.Errorf("expected type=preExecution, got %s", record["type"])
	}
	if record["operation"] != "beaconRootStorage" {
		t.Errorf("expected operation=beaconRootStorage, got %s", record["operation"])
	}
	if record["eip"] != "4788" {
		t.Errorf("expected eip=4788, got %s", record["eip"])
	}
	if record["gasUsed"] != "0x186a0" { // 100000 in hex
		t.Errorf("expected gasUsed=0x186a0, got %s", record["gasUsed"])
	}

	// Verify ringBuffer object
	ringBuffer, ok := record["ringBuffer"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ringBuffer field to be a map")
	}
	if ringBuffer["index"] != "0x3e8" {
		t.Errorf("expected ringBuffer.index=0x3e8, got %s", ringBuffer["index"])
	}
	if ringBuffer["timestampSlot"] != "0x7d0" {
		t.Errorf("expected ringBuffer.timestampSlot=0x7d0, got %s", ringBuffer["timestampSlot"])
	}
	if ringBuffer["rootSlot"] != "0x7d1" {
		t.Errorf("expected ringBuffer.rootSlot=0x7d1, got %s", ringBuffer["rootSlot"])
	}

	// Verify storageWrites array
	storageWrites, ok := record["storageWrites"].([]interface{})
	if !ok {
		t.Fatal("expected storageWrites field to be an array")
	}
	if len(storageWrites) != 2 {
		t.Fatalf("expected 2 storage writes, got %d", len(storageWrites))
	}

	// Verify first storage write (timestamp slot)
	write0 := storageWrites[0].(map[string]interface{})
	if write0["slot"] != "0x7d0" {
		t.Errorf("expected slot=0x7d0, got %s", write0["slot"])
	}
	if write0["oldValue"] != "0x0" {
		t.Errorf("expected oldValue=0x0, got %s", write0["oldValue"])
	}
	if write0["newValue"] != "0x3e8" {
		t.Errorf("expected newValue=0x3e8, got %s", write0["newValue"])
	}

	// Verify second storage write (root slot)
	write1 := storageWrites[1].(map[string]interface{})
	if write1["slot"] != "0x7d1" {
		t.Errorf("expected slot=0x7d1, got %s", write1["slot"])
	}
	if write1["oldValue"] != "0x0" {
		t.Errorf("expected oldValue=0x0, got %s", write1["oldValue"])
	}

	// Verify internal fields starting with "_" are NOT in the output
	if _, exists := record["_oldTimestamp"]; exists {
		t.Error("expected _oldTimestamp to be filtered out")
	}
	if _, exists := record["_oldRoot"]; exists {
		t.Error("expected _oldRoot to be filtered out")
	}
}

// Helper function to parse JSONL records
func parseRecords(t *testing.T, lines [][]byte) []map[string]interface{} {
	t.Helper()
	var records []map[string]interface{}

	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var record map[string]interface{}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("failed to parse JSON record: %v\nLine: %s", err, string(line))
		}
		records = append(records, record)
	}

	return records
}
