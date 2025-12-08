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
	"testing"
)

// TestSaveEnhancedCorpusEntry_EESTFormat verifies that metadata is injected
// inside test objects following the EEST standard format.
func TestSaveEnhancedCorpusEntry_EESTFormat(t *testing.T) {
	// Create a temporary directory for the test
	tempDir, err := os.MkdirTemp("", "statetest-corpus-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create corpus saver
	saver, err := NewCorpusSaver(tempDir)
	if err != nil {
		t.Fatalf("failed to create corpus saver: %v", err)
	}

	// Sample state test JSON
	testJSON := []byte(`{
		"testName1": {
			"env": {"currentNumber": "0x01"},
			"pre": {},
			"transaction": {},
			"post": {}
		}
	}`)

	// Mock tracing result
	traceResult := &TracingResult{
		TraceHash:  "abcdef1234567890abcdef1234567890",
		StateRoot:  "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		TraceLines: 42,
		GasUsed:    21000,
	}

	// Save the enhanced corpus entry
	savedPath, err := saver.SaveEnhancedCorpusEntry(testJSON, traceResult, 0.001, "test-strategy")
	if err != nil {
		t.Fatalf("SaveEnhancedCorpusEntry failed: %v", err)
	}

	// Read the saved file
	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	// Parse and verify the structure
	var saved map[string]json.RawMessage
	if err := json.Unmarshal(savedData, &saved); err != nil {
		t.Fatalf("failed to parse saved JSON: %v", err)
	}

	// Verify no top-level _crossvm key
	if _, ok := saved["_crossvm"]; ok {
		t.Error("saved JSON should NOT have top-level _crossvm key (legacy format)")
	}

	// Verify test object exists
	testRaw, ok := saved["testName1"]
	if !ok {
		t.Fatal("saved JSON should have testName1 test object")
	}

	// Parse test object
	var testObj map[string]json.RawMessage
	if err := json.Unmarshal(testRaw, &testObj); err != nil {
		t.Fatalf("failed to parse test object: %v", err)
	}

	// Verify _info exists inside test object
	infoRaw, ok := testObj["_info"]
	if !ok {
		t.Fatal("test object should have _info field (EEST standard format)")
	}

	// Parse and verify _info content
	var meta CrossVMMetadata
	if err := json.Unmarshal(infoRaw, &meta); err != nil {
		t.Fatalf("failed to parse _info: %v", err)
	}

	// Verify metadata fields
	if meta.GeneratedBy != "geth" {
		t.Errorf("expected generatedBy='geth', got '%s'", meta.GeneratedBy)
	}
	if meta.TraceHash != traceResult.TraceHash {
		t.Errorf("expected traceHash='%s', got '%s'", traceResult.TraceHash, meta.TraceHash)
	}
	if meta.StateRoot != traceResult.StateRoot {
		t.Errorf("expected stateRoot='%s', got '%s'", traceResult.StateRoot, meta.StateRoot)
	}
	if meta.CrossVMVersion != CrossVMMetadataVersion {
		t.Errorf("expected crossvmVersion='%s', got '%s'", CrossVMMetadataVersion, meta.CrossVMVersion)
	}
	if meta.Comment != CrossVMDefaultComment {
		t.Errorf("expected comment='%s', got '%s'", CrossVMDefaultComment, meta.Comment)
	}
	if meta.TraceLines != traceResult.TraceLines {
		t.Errorf("expected traceLines=%d, got %d", traceResult.TraceLines, meta.TraceLines)
	}
	if meta.GeneratedAt == "" {
		t.Error("expected generatedAt to be set")
	}

	t.Logf("Successfully saved EEST-format corpus entry to: %s", savedPath)
}

// TestSaveEnhancedCorpusEntry_MultipleTests verifies that metadata is injected
// into each test object when there are multiple tests.
func TestSaveEnhancedCorpusEntry_MultipleTests(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "statetest-corpus-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	saver, err := NewCorpusSaver(tempDir)
	if err != nil {
		t.Fatalf("failed to create corpus saver: %v", err)
	}

	// Multiple tests in one file
	testJSON := []byte(`{
		"test1": {"env": {}, "pre": {}, "transaction": {}, "post": {}},
		"test2": {"env": {}, "pre": {}, "transaction": {}, "post": {}}
	}`)

	traceResult := &TracingResult{
		TraceHash:  "1234567890abcdef1234567890abcdef",
		StateRoot:  "0xabcdef",
		TraceLines: 10,
	}

	savedPath, err := saver.SaveEnhancedCorpusEntry(testJSON, traceResult, 0.001, "test")
	if err != nil {
		t.Fatalf("SaveEnhancedCorpusEntry failed: %v", err)
	}

	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	var saved map[string]json.RawMessage
	if err := json.Unmarshal(savedData, &saved); err != nil {
		t.Fatalf("failed to parse saved JSON: %v", err)
	}

	// Verify both tests have _info
	for _, testName := range []string{"test1", "test2"} {
		testRaw, ok := saved[testName]
		if !ok {
			t.Errorf("missing test '%s'", testName)
			continue
		}

		var testObj map[string]json.RawMessage
		if err := json.Unmarshal(testRaw, &testObj); err != nil {
			t.Errorf("failed to parse test '%s': %v", testName, err)
			continue
		}

		if _, ok := testObj["_info"]; !ok {
			t.Errorf("test '%s' should have _info field", testName)
		}
	}
}

// TestExtractCrossVMMetadata_EESTFormat verifies extraction from EEST standard format.
func TestExtractCrossVMMetadata_EESTFormat(t *testing.T) {
	// EEST standard format: _info inside test object
	testJSON := []byte(`{
		"testName": {
			"_info": {
				"comment": "Cross-VM consensus verification test",
				"generatedBy": "nethermind",
				"traceHash": "hash123",
				"stateRoot": "0xroot",
				"crossvmVersion": "1.0",
				"traceLines": 50
			},
			"env": {},
			"pre": {},
			"transaction": {},
			"post": {}
		}
	}`)

	meta := extractCrossVMMetadata(testJSON)
	if meta == nil {
		t.Fatal("extractCrossVMMetadata returned nil for EEST format")
	}

	if meta.GeneratedBy != "nethermind" {
		t.Errorf("expected generatedBy='nethermind', got '%s'", meta.GeneratedBy)
	}
	if meta.TraceHash != "hash123" {
		t.Errorf("expected traceHash='hash123', got '%s'", meta.TraceHash)
	}
	if meta.StateRoot != "0xroot" {
		t.Errorf("expected stateRoot='0xroot', got '%s'", meta.StateRoot)
	}
	if meta.CrossVMVersion != "1.0" {
		t.Errorf("expected crossvmVersion='1.0', got '%s'", meta.CrossVMVersion)
	}
}

// TestExtractCrossVMMetadata_LegacyFormat verifies backward compatibility with legacy format.
func TestExtractCrossVMMetadata_LegacyFormat(t *testing.T) {
	// Legacy format: _crossvm at top level
	testJSON := []byte(`{
		"testName": {
			"env": {},
			"pre": {},
			"transaction": {},
			"post": {}
		},
		"_crossvm": {
			"generatedBy": "besu",
			"traceHash": "legacyhash",
			"stateRoot": "0xlegacyroot"
		}
	}`)

	meta := extractCrossVMMetadata(testJSON)
	if meta == nil {
		t.Fatal("extractCrossVMMetadata returned nil for legacy format")
	}

	if meta.GeneratedBy != "besu" {
		t.Errorf("expected generatedBy='besu', got '%s'", meta.GeneratedBy)
	}
	if meta.TraceHash != "legacyhash" {
		t.Errorf("expected traceHash='legacyhash', got '%s'", meta.TraceHash)
	}
}

// TestExtractCrossVMMetadata_PreferEEST verifies EEST format is preferred over legacy.
func TestExtractCrossVMMetadata_PreferEEST(t *testing.T) {
	// Both formats present - EEST should be preferred
	testJSON := []byte(`{
		"testName": {
			"_info": {
				"generatedBy": "geth",
				"traceHash": "eesthash",
				"stateRoot": "0xeestroot"
			},
			"env": {},
			"pre": {}
		},
		"_crossvm": {
			"generatedBy": "legacy",
			"traceHash": "legacyhash",
			"stateRoot": "0xlegacyroot"
		}
	}`)

	meta := extractCrossVMMetadata(testJSON)
	if meta == nil {
		t.Fatal("extractCrossVMMetadata returned nil")
	}

	// Should return EEST format data (from inside test object)
	if meta.GeneratedBy != "geth" {
		t.Errorf("expected EEST format generatedBy='geth', got '%s'", meta.GeneratedBy)
	}
	if meta.TraceHash != "eesthash" {
		t.Errorf("expected EEST format traceHash='eesthash', got '%s'", meta.TraceHash)
	}
}

// TestStripCrossVMMetadata_EESTFormat verifies stripping from EEST standard format.
func TestStripCrossVMMetadata_EESTFormat(t *testing.T) {
	testJSON := []byte(`{
		"testName": {
			"_info": {
				"generatedBy": "geth",
				"traceHash": "hash123",
				"stateRoot": "0xroot"
			},
			"env": {"currentNumber": "0x01"},
			"pre": {}
		}
	}`)

	stripped, err := StripCrossVMMetadata(testJSON)
	if err != nil {
		t.Fatalf("StripCrossVMMetadata failed: %v", err)
	}

	// Verify _info is removed
	var result map[string]json.RawMessage
	if err := json.Unmarshal(stripped, &result); err != nil {
		t.Fatalf("failed to parse stripped JSON: %v", err)
	}

	testRaw, ok := result["testName"]
	if !ok {
		t.Fatal("testName should still exist after stripping")
	}

	var testObj map[string]json.RawMessage
	if err := json.Unmarshal(testRaw, &testObj); err != nil {
		t.Fatalf("failed to parse test object: %v", err)
	}

	if _, ok := testObj["_info"]; ok {
		t.Error("_info should be stripped from test object")
	}

	// Verify other fields are preserved
	if _, ok := testObj["env"]; !ok {
		t.Error("env should be preserved")
	}
}

// TestStripCrossVMMetadata_LegacyFormat verifies stripping legacy format.
func TestStripCrossVMMetadata_LegacyFormat(t *testing.T) {
	testJSON := []byte(`{
		"testName": {
			"env": {},
			"pre": {}
		},
		"_crossvm": {
			"generatedBy": "geth",
			"traceHash": "hash123"
		}
	}`)

	stripped, err := StripCrossVMMetadata(testJSON)
	if err != nil {
		t.Fatalf("StripCrossVMMetadata failed: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(stripped, &result); err != nil {
		t.Fatalf("failed to parse stripped JSON: %v", err)
	}

	if _, ok := result["_crossvm"]; ok {
		t.Error("_crossvm should be stripped")
	}
	if _, ok := result["testName"]; !ok {
		t.Error("testName should be preserved")
	}
}

// TestStripCrossVMMetadata_BothFormats verifies stripping when both formats present.
func TestStripCrossVMMetadata_BothFormats(t *testing.T) {
	testJSON := []byte(`{
		"testName": {
			"_info": {
				"generatedBy": "geth",
				"traceHash": "hash123"
			},
			"env": {}
		},
		"_crossvm": {
			"generatedBy": "legacy",
			"traceHash": "legacyhash"
		}
	}`)

	stripped, err := StripCrossVMMetadata(testJSON)
	if err != nil {
		t.Fatalf("StripCrossVMMetadata failed: %v", err)
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(stripped, &result); err != nil {
		t.Fatalf("failed to parse stripped JSON: %v", err)
	}

	// Both should be removed
	if _, ok := result["_crossvm"]; ok {
		t.Error("_crossvm should be stripped")
	}

	testRaw, ok := result["testName"]
	if !ok {
		t.Fatal("testName should be preserved")
	}

	var testObj map[string]json.RawMessage
	if err := json.Unmarshal(testRaw, &testObj); err != nil {
		t.Fatalf("failed to parse test object: %v", err)
	}

	if _, ok := testObj["_info"]; ok {
		t.Error("_info should be stripped from test object")
	}
}

// TestLoadEnhancedCorpus_EESTFormat verifies loading corpus with EEST format.
func TestLoadEnhancedCorpus_EESTFormat(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "statetest-corpus-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file with EEST format
	testJSON := `{
		"testName": {
			"_info": {
				"comment": "Test",
				"generatedBy": "revm",
				"traceHash": "revmhash123",
				"stateRoot": "0xrevmroot",
				"crossvmVersion": "1.0"
			},
			"env": {},
			"pre": {}
		}
	}`

	testFile := filepath.Join(tempDir, "test.json")
	if err := os.WriteFile(testFile, []byte(testJSON), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	entries, err := LoadEnhancedCorpus(tempDir)
	if err != nil {
		t.Fatalf("LoadEnhancedCorpus failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Metadata == nil {
		t.Fatal("entry should have metadata")
	}

	if entry.Metadata.GeneratedBy != "revm" {
		t.Errorf("expected generatedBy='revm', got '%s'", entry.Metadata.GeneratedBy)
	}
	if entry.Metadata.TraceHash != "revmhash123" {
		t.Errorf("expected traceHash='revmhash123', got '%s'", entry.Metadata.TraceHash)
	}
}

// TestLoadEnhancedCorpus_LegacyFormat verifies loading corpus with legacy format.
func TestLoadEnhancedCorpus_LegacyFormat(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "statetest-corpus-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file with legacy format
	testJSON := `{
		"testName": {
			"env": {},
			"pre": {}
		},
		"_crossvm": {
			"generatedBy": "erigon",
			"traceHash": "erigonhash"
		}
	}`

	testFile := filepath.Join(tempDir, "test.json")
	if err := os.WriteFile(testFile, []byte(testJSON), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	entries, err := LoadEnhancedCorpus(tempDir)
	if err != nil {
		t.Fatalf("LoadEnhancedCorpus failed: %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.Metadata == nil {
		t.Fatal("entry should have metadata (legacy format)")
	}

	if entry.Metadata.GeneratedBy != "erigon" {
		t.Errorf("expected generatedBy='erigon', got '%s'", entry.Metadata.GeneratedBy)
	}
}

// TestCrossVMMetadataVersion verifies the version constant.
func TestCrossVMMetadataVersion(t *testing.T) {
	if CrossVMMetadataVersion != "1.0" {
		t.Errorf("expected CrossVMMetadataVersion='1.0', got '%s'", CrossVMMetadataVersion)
	}
}

// TestCrossVMDefaultComment verifies the default comment constant.
func TestCrossVMDefaultComment(t *testing.T) {
	expected := "Cross-VM consensus verification test"
	if CrossVMDefaultComment != expected {
		t.Errorf("expected CrossVMDefaultComment='%s', got '%s'", expected, CrossVMDefaultComment)
	}
}

// TestEESTFormatJSONStructure verifies the exact JSON structure matches EEST spec.
func TestEESTFormatJSONStructure(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "statetest-corpus-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	saver, err := NewCorpusSaver(tempDir)
	if err != nil {
		t.Fatalf("failed to create corpus saver: %v", err)
	}

	testJSON := []byte(`{"myTest": {"env": {}, "pre": {}}}`)
	traceResult := &TracingResult{
		TraceHash: "abcdef1234567890abcdef1234567890",
		StateRoot: "0xroot",
	}

	savedPath, err := saver.SaveEnhancedCorpusEntry(testJSON, traceResult, 0.001, "test")
	if err != nil {
		t.Fatalf("SaveEnhancedCorpusEntry failed: %v", err)
	}

	savedData, err := os.ReadFile(savedPath)
	if err != nil {
		t.Fatalf("failed to read saved file: %v", err)
	}

	// Verify the JSON structure contains expected keys in order
	savedStr := string(savedData)

	// Should have _info inside myTest
	if !strings.Contains(savedStr, `"_info"`) {
		t.Error("saved JSON should contain '_info' key")
	}

	// Should have required fields
	requiredFields := []string{
		`"comment"`,
		`"generatedBy"`,
		`"traceHash"`,
		`"stateRoot"`,
		`"crossvmVersion"`,
	}

	for _, field := range requiredFields {
		if !strings.Contains(savedStr, field) {
			t.Errorf("saved JSON should contain %s field", field)
		}
	}

	// Should NOT have _crossvm at top level
	// Parse to verify structure
	var parsed map[string]interface{}
	if err := json.Unmarshal(savedData, &parsed); err != nil {
		t.Fatalf("failed to parse saved JSON: %v", err)
	}

	if _, ok := parsed["_crossvm"]; ok {
		t.Error("saved JSON should NOT have top-level _crossvm")
	}

	// Verify myTest has _info
	myTest, ok := parsed["myTest"].(map[string]interface{})
	if !ok {
		t.Fatal("myTest should be a map")
	}

	info, ok := myTest["_info"].(map[string]interface{})
	if !ok {
		t.Fatal("myTest should have _info as a map")
	}

	// Verify _info contains the expected values
	if info["generatedBy"] != "geth" {
		t.Errorf("expected generatedBy='geth', got '%v'", info["generatedBy"])
	}
	if info["crossvmVersion"] != CrossVMMetadataVersion {
		t.Errorf("expected crossvmVersion='%s', got '%v'", CrossVMMetadataVersion, info["crossvmVersion"])
	}

	t.Logf("EEST-compliant JSON structure verified")
}

// TestIsCrossVMEntry_CaseInsensitive verifies case-insensitive generatedBy comparison.
func TestIsCrossVMEntry_CaseInsensitive(t *testing.T) {
	testCases := []struct {
		generatedBy string
		currentVM   string
		isCrossVM   bool
	}{
		// Same VM - should NOT be cross-VM
		{"geth", "geth", false},
		{"GETH", "geth", false},     // Case insensitive
		{"Geth", "geth", false},     // Case insensitive
		{"besu", "besu", false},
		{"BESU", "besu", false},
		{"Besu", "BESU", false},

		// Different VMs - should be cross-VM
		{"besu", "geth", true},
		{"BESU", "geth", true},
		{"nethermind", "geth", true},
		{"erigon", "besu", true},
		{"revm", "geth", true},
		{"evmone", "besu", true},

		// Empty generatedBy - should NOT be cross-VM
		{"", "geth", false},
	}

	for _, tc := range testCases {
		entry := EnhancedCorpusEntry{
			Metadata: &CrossVMMetadata{
				GeneratedBy: tc.generatedBy,
				TraceHash:   "somehash",
			},
		}
		if tc.generatedBy == "" {
			entry.Metadata.GeneratedBy = ""
		}

		result := entry.IsCrossVMEntry(tc.currentVM)
		if result != tc.isCrossVM {
			t.Errorf("IsCrossVMEntry(%q, %q) = %v, want %v",
				tc.generatedBy, tc.currentVM, result, tc.isCrossVM)
		}
	}
}

// TestIsCrossVMEntry_NilMetadata verifies nil metadata handling.
func TestIsCrossVMEntry_NilMetadata(t *testing.T) {
	entry := EnhancedCorpusEntry{
		Metadata: nil,
	}

	if entry.IsCrossVMEntry("geth") {
		t.Error("IsCrossVMEntry should return false for nil metadata")
	}
}
