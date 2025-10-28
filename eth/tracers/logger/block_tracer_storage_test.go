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
	"strings"
	"testing"
)

// TestStorageWritesInPreExecution verifies that storage writes are included in preExecution records
// when passed via the OnPreExecutionEnd metadata parameter.
func TestStorageWritesInPreExecution(t *testing.T) {
	var buf bytes.Buffer

	// Create tracer
	config := &BlockTracerConfig{
		Config:       &Config{},
		Level:        TraceLevelStandard,
		IncludeBlock: true,
		IncludeTx:    false,
	}
	tracer := NewBlockTracer(config, &buf)

	// Simulate EIP-4788 beacon root storage call
	metadata := map[string]interface{}{
		"timestamp":             "0x3e8",
		"parentBeaconBlockRoot": "0x6c31fc174f0107e0b163c3d8876e121b51c2793d9b7d580bb1147ba0e2a8bdbe",
		"contractAddress":       "0x000f3df6d732807ef1319fb7b8bb8522d0beac02",
		"ringBuffer": map[string]interface{}{
			"index":         "0x3e8",
			"timestampSlot": "0x3e8",
			"rootSlot":      "0x23e7",
		},
	}

	// Call OnPreExecutionStart
	tracer.Hooks.OnPreExecutionStart("beaconRootStorage", "4788", metadata)

	// Simulate storage writes metadata from state_processor.go
	endMetadata := map[string]interface{}{
		"storageWrites": []map[string]interface{}{
			{
				"slot":     "0x3e8",
				"oldValue": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"newValue": "0x00000000000000000000000000000000000000000000000000000000000003e8",
			},
			{
				"slot":     "0x23e7",
				"oldValue": "0x0000000000000000000000000000000000000000000000000000000000000000",
				"newValue": "0x6c31fc174f0107e0b163c3d8876e121b51c2793d9b7d580bb1147ba0e2a8bdbe",
			},
		},
	}

	// Call OnPreExecutionEnd with storage writes
	tracer.Hooks.OnPreExecutionEnd(94440, endMetadata)

	// Parse output
	output := buf.String()
	if output == "" {
		t.Fatal("No trace output generated")
	}

	// Find preExecution record
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var preExecRecord map[string]interface{}
	for _, line := range lines {
		var record map[string]interface{}
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record["type"] == "preExecution" {
			preExecRecord = record
			break
		}
	}

	if preExecRecord == nil {
		t.Fatal("No preExecution record found in output")
	}

	// Verify storageWrites field exists
	storageWrites, ok := preExecRecord["storageWrites"]
	if !ok {
		t.Fatalf("storageWrites field not found in preExecution record. Record: %+v", preExecRecord)
	}

	// Verify storageWrites is an array
	writes, ok := storageWrites.([]interface{})
	if !ok {
		t.Fatalf("storageWrites is not an array: %T", storageWrites)
	}

	// Verify we have 2 storage writes
	if len(writes) != 2 {
		t.Fatalf("Expected 2 storage writes, got %d", len(writes))
	}

	// Verify first write (timestamp slot)
	write1 := writes[0].(map[string]interface{})
	if write1["slot"] != "0x3e8" {
		t.Errorf("First write slot mismatch: got %v, want 0x3e8", write1["slot"])
	}
	if write1["oldValue"] != "0x0000000000000000000000000000000000000000000000000000000000000000" {
		t.Errorf("First write oldValue mismatch: got %v", write1["oldValue"])
	}
	if write1["newValue"] != "0x00000000000000000000000000000000000000000000000000000000000003e8" {
		t.Errorf("First write newValue mismatch: got %v", write1["newValue"])
	}

	// Verify second write (root slot)
	write2 := writes[1].(map[string]interface{})
	if write2["slot"] != "0x23e7" {
		t.Errorf("Second write slot mismatch: got %v, want 0x23e7", write2["slot"])
	}
	if write2["newValue"] != "0x6c31fc174f0107e0b163c3d8876e121b51c2793d9b7d580bb1147ba0e2a8bdbe" {
		t.Errorf("Second write newValue mismatch: got %v", write2["newValue"])
	}

	t.Log("✓ Storage writes successfully captured in preExecution record")
}
