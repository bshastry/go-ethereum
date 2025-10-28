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
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

// TestBlockTracerScopeFiltering tests the IncludeTx and IncludeBlock scope filters.
func TestBlockTracerScopeFiltering(t *testing.T) {
	tests := []struct {
		name                string
		level               TraceLevel
		includeTx           bool
		includeBlock        bool
		expectedRecordTypes []string
		unexpectedTypes     []string
	}{
		{
			name:                "Both enabled (default)",
			level:               TraceLevelMinimal,
			includeTx:           true,
			includeBlock:        true,
			expectedRecordTypes: []string{"blockStart", "txStart", "txEnd", "blockEnd"},
			unexpectedTypes:     []string{},
		},
		{
			name:                "Only block traces",
			level:               TraceLevelMinimal,
			includeTx:           false,
			includeBlock:        true,
			expectedRecordTypes: []string{"blockStart", "blockEnd"},
			unexpectedTypes:     []string{"txStart", "txEnd"},
		},
		{
			name:                "Only tx traces",
			level:               TraceLevelMinimal,
			includeTx:           true,
			includeBlock:        false,
			expectedRecordTypes: []string{"txStart", "txEnd"},
			unexpectedTypes:     []string{"blockStart", "blockEnd"},
		},
		{
			name:                "Neither enabled (no traces)",
			level:               TraceLevelMinimal,
			includeTx:           false,
			includeBlock:        false,
			expectedRecordTypes: []string{},
			unexpectedTypes:     []string{"blockStart", "blockEnd", "txStart", "txEnd"},
		},
		{
			name:                "Standard level - block only",
			level:               TraceLevelStandard,
			includeTx:           false,
			includeBlock:        true,
			expectedRecordTypes: []string{"blockStart", "blockEnd", "preExecution", "postExecution", "validation"},
			unexpectedTypes:     []string{"txStart", "txEnd"},
		},
		{
			name:                "Standard level - tx only",
			level:               TraceLevelStandard,
			includeTx:           true,
			includeBlock:        false,
			expectedRecordTypes: []string{"txStart", "txEnd"},
			unexpectedTypes:     []string{"blockStart", "blockEnd", "preExecution", "postExecution", "validation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &BlockTracerConfig{
				Config:       &Config{},
				Level:        tt.level,
				IncludeTx:    tt.includeTx,
				IncludeBlock: tt.includeBlock,
			}

			hooks := NewBlockTracer(cfg, &buf)

			// Simulate block execution
			header := &types.Header{
				Number:     big.NewInt(1),
				GasLimit:   10000000,
				Time:       1234567890,
				Difficulty: big.NewInt(0),
			}
			block := types.NewBlock(header, nil, nil, nil)

			// Emit block start
			if hooks.OnBlockStart != nil {
				hooks.OnBlockStart(tracing.BlockEvent{Block: block})
			}

			// Emit pre-execution (only if Standard+)
			if hooks.OnPreExecutionStart != nil {
				hooks.OnPreExecutionStart("beaconRootStorage", "4788", map[string]interface{}{
					"timestamp": uint64(1234567890),
				})
			}
			if hooks.OnPreExecutionEnd != nil {
				hooks.OnPreExecutionEnd(30_000_000, nil)
			}

			// Emit tx start/end
			tx := types.NewTransaction(0, common.Address{0x01}, big.NewInt(1000), 21000, big.NewInt(1), nil)
			vmctx := &tracing.VMContext{
				BlockNumber: big.NewInt(1),
				Time:        1234567890,
			}
			if hooks.OnTxStart != nil {
				hooks.OnTxStart(vmctx, tx, common.Address{0x02})
			}

			receipt := &types.Receipt{
				Status:            1,
				GasUsed:           21000,
				CumulativeGasUsed: 21000,
			}
			if hooks.OnTxEnd != nil {
				hooks.OnTxEnd(receipt, nil)
			}

			// Emit validation (only if Standard+)
			if hooks.OnValidation != nil {
				hooks.OnValidation("gasAccounting", map[string]interface{}{
					"blockGasLimit": uint64(10000000),
					"totalGasUsed":  uint64(21000),
				})
			}

			// Emit post-execution (only if Standard+)
			if hooks.OnPostExecutionStart != nil {
				hooks.OnPostExecutionStart("withdrawals", "4895")
			}
			if hooks.OnPostExecutionEnd != nil {
				hooks.OnPostExecutionEnd(map[string]interface{}{
					"withdrawalCount": 0,
				})
			}

			// Emit block end
			if hooks.OnBlockEnd != nil {
				hooks.OnBlockEnd(nil)
			}

			// Parse output and verify
			output := buf.String()
			lines := strings.Split(strings.TrimSpace(output), "\n")

			// Count record types
			recordTypes := make(map[string]int)
			for _, line := range lines {
				if line == "" {
					continue
				}
				var record map[string]interface{}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("Failed to parse JSON: %v\nLine: %s", err, line)
				}
				if typ, ok := record["type"].(string); ok {
					recordTypes[typ]++
				}
			}

			// Verify expected types are present
			for _, expectedType := range tt.expectedRecordTypes {
				if recordTypes[expectedType] == 0 {
					t.Errorf("Expected record type %q not found in output. Got types: %v", expectedType, recordTypes)
				}
			}

			// Verify unexpected types are absent
			for _, unexpectedType := range tt.unexpectedTypes {
				if recordTypes[unexpectedType] > 0 {
					t.Errorf("Unexpected record type %q found in output. Got types: %v", unexpectedType, recordTypes)
				}
			}
		})
	}
}

// TestBlockTracerCustomLevelWithScopeFilters tests custom level with scope filters.
func TestBlockTracerCustomLevelWithScopeFilters(t *testing.T) {
	tests := []struct {
		name            string
		customTypes     []string
		includeTx       bool
		includeBlock    bool
		expectedTypes   []string
		unexpectedTypes []string
	}{
		{
			name:            "Custom: blockStart,txStart with block-only filter",
			customTypes:     []string{"blockStart", "txStart"},
			includeTx:       false,
			includeBlock:    true,
			expectedTypes:   []string{"blockStart"},
			unexpectedTypes: []string{"txStart"},
		},
		{
			name:            "Custom: blockStart,txStart with tx-only filter",
			customTypes:     []string{"blockStart", "txStart"},
			includeTx:       true,
			includeBlock:    false,
			expectedTypes:   []string{"txStart"},
			unexpectedTypes: []string{"blockStart"},
		},
		{
			name:            "Custom: all types with tx-only filter",
			customTypes:     []string{"blockStart", "txStart", "preExecution", "validation", "txEnd", "blockEnd"},
			includeTx:       true,
			includeBlock:    false,
			expectedTypes:   []string{"txStart", "txEnd"},
			unexpectedTypes: []string{"blockStart", "blockEnd", "preExecution", "validation"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &BlockTracerConfig{
				Config:       &Config{},
				Level:        TraceLevelCustom,
				CustomTypes:  tt.customTypes,
				IncludeTx:    tt.includeTx,
				IncludeBlock: tt.includeBlock,
			}

			hooks := NewBlockTracer(cfg, &buf)

			// Simulate block execution
			header := &types.Header{
				Number:     big.NewInt(1),
				GasLimit:   10000000,
				Time:       1234567890,
				Difficulty: big.NewInt(0),
			}
			block := types.NewBlock(header, nil, nil, nil)

			// Emit all possible events
			if hooks.OnBlockStart != nil {
				hooks.OnBlockStart(tracing.BlockEvent{Block: block})
			}
			if hooks.OnPreExecutionStart != nil {
				hooks.OnPreExecutionStart("beaconRootStorage", "4788", map[string]interface{}{})
			}
			if hooks.OnPreExecutionEnd != nil {
				hooks.OnPreExecutionEnd(0, nil)
			}

			tx := types.NewTransaction(0, common.Address{0x01}, big.NewInt(1000), 21000, big.NewInt(1), nil)
			vmctx := &tracing.VMContext{BlockNumber: big.NewInt(1), Time: 1234567890}
			if hooks.OnTxStart != nil {
				hooks.OnTxStart(vmctx, tx, common.Address{0x02})
			}

			receipt := &types.Receipt{Status: 1, GasUsed: 21000, CumulativeGasUsed: 21000}
			if hooks.OnTxEnd != nil {
				hooks.OnTxEnd(receipt, nil)
			}

			if hooks.OnValidation != nil {
				hooks.OnValidation("gasAccounting", map[string]interface{}{})
			}

			if hooks.OnBlockEnd != nil {
				hooks.OnBlockEnd(nil)
			}

			// Parse and verify
			output := buf.String()
			lines := strings.Split(strings.TrimSpace(output), "\n")

			recordTypes := make(map[string]bool)
			for _, line := range lines {
				if line == "" {
					continue
				}
				var record map[string]interface{}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("Failed to parse JSON: %v", err)
				}
				if typ, ok := record["type"].(string); ok {
					recordTypes[typ] = true
				}
			}

			// Verify expected types
			for _, expectedType := range tt.expectedTypes {
				if !recordTypes[expectedType] {
					t.Errorf("Expected type %q not found. Got: %v", expectedType, recordTypes)
				}
			}

			// Verify unexpected types are absent
			for _, unexpectedType := range tt.unexpectedTypes {
				if recordTypes[unexpectedType] {
					t.Errorf("Unexpected type %q found. Got: %v", unexpectedType, recordTypes)
				}
			}
		})
	}
}

// TestHookInstallation verifies that hooks are only installed when appropriate.
func TestHookInstallation(t *testing.T) {
	tests := []struct {
		name         string
		includeTx    bool
		includeBlock bool
		level        TraceLevel
		checkHooks   func(*testing.T, *tracing.Hooks)
	}{
		{
			name:         "Block-only: no tx hooks installed",
			includeTx:    false,
			includeBlock: true,
			level:        TraceLevelMinimal,
			checkHooks: func(t *testing.T, h *tracing.Hooks) {
				if h.OnTxStart != nil {
					t.Error("OnTxStart should be nil when IncludeTx=false")
				}
				if h.OnTxEnd != nil {
					t.Error("OnTxEnd should be nil when IncludeTx=false")
				}
				if h.OnBlockStart == nil {
					t.Error("OnBlockStart should not be nil when IncludeBlock=true")
				}
				if h.OnBlockEnd == nil {
					t.Error("OnBlockEnd should not be nil when IncludeBlock=true")
				}
			},
		},
		{
			name:         "Tx-only: no block hooks installed",
			includeTx:    true,
			includeBlock: false,
			level:        TraceLevelStandard,
			checkHooks: func(t *testing.T, h *tracing.Hooks) {
				if h.OnBlockStart != nil {
					t.Error("OnBlockStart should be nil when IncludeBlock=false")
				}
				if h.OnBlockEnd != nil {
					t.Error("OnBlockEnd should be nil when IncludeBlock=false")
				}
				if h.OnPreExecutionStart != nil {
					t.Error("OnPreExecutionStart should be nil when IncludeBlock=false")
				}
				if h.OnValidation != nil {
					t.Error("OnValidation should be nil when IncludeBlock=false")
				}
				if h.OnTxStart == nil {
					t.Error("OnTxStart should not be nil when IncludeTx=true")
				}
				if h.OnTxEnd == nil {
					t.Error("OnTxEnd should not be nil when IncludeTx=true")
				}
			},
		},
		{
			name:         "Neither enabled: no hooks installed",
			includeTx:    false,
			includeBlock: false,
			level:        TraceLevelFull,
			checkHooks: func(t *testing.T, h *tracing.Hooks) {
				if h.OnBlockStart != nil || h.OnBlockEnd != nil ||
					h.OnTxStart != nil || h.OnTxEnd != nil ||
					h.OnPreExecutionStart != nil || h.OnValidation != nil ||
					h.OnOpcode != nil || h.OnTrieOperation != nil {
					t.Error("No hooks should be installed when both filters are disabled")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &BlockTracerConfig{
				Config:        &Config{},
				Level:         tt.level,
				IncludeTx:     tt.includeTx,
				IncludeBlock:  tt.includeBlock,
				IncludeOpcode: true,
				IncludeTrie:   true,
			}

			hooks := NewBlockTracer(cfg, &buf)
			tt.checkHooks(t, hooks.Hooks)
		})
	}
}
