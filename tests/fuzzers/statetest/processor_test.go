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
	"time"
)

func TestInjectCrossVMMetadata(t *testing.T) {
	// Create a minimal test JSON
	testJSON := []byte(`{
  "testName": {
    "env": {"currentCoinbase": "0x1234"},
    "pre": {},
    "transaction": {},
    "post": {"London": []}
  }
}`)

	meta := &CrossVMMetadata{
		GeneratedBy:    "geth",
		TraceHash:      "abc123def456",
		StateRoot:      "0xdeadbeef",
		CrossVMVersion: "1.0",
		TraceLines:     42,
		GeneratedAt:    "2025-01-01T00:00:00Z",
		Version:        "test",
	}

	result, err := InjectCrossVMMetadata(testJSON, meta)
	if err != nil {
		t.Fatalf("InjectCrossVMMetadata failed: %v", err)
	}

	// Parse result to verify structure
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// Check that testName key exists
	testRaw, ok := parsed["testName"]
	if !ok {
		t.Fatal("testName key missing from result")
	}

	// Parse test object
	var testObj map[string]json.RawMessage
	if err := json.Unmarshal(testRaw, &testObj); err != nil {
		t.Fatalf("Failed to parse test object: %v", err)
	}

	// Check that _info was injected
	infoRaw, ok := testObj["_info"]
	if !ok {
		t.Fatal("_info key missing from test object")
	}

	// Parse _info
	var info CrossVMMetadata
	if err := json.Unmarshal(infoRaw, &info); err != nil {
		t.Fatalf("Failed to parse _info: %v", err)
	}

	// Verify metadata fields
	if info.TraceHash != meta.TraceHash {
		t.Errorf("TraceHash mismatch: got %s, want %s", info.TraceHash, meta.TraceHash)
	}
	if info.GeneratedBy != meta.GeneratedBy {
		t.Errorf("GeneratedBy mismatch: got %s, want %s", info.GeneratedBy, meta.GeneratedBy)
	}
	if info.StateRoot != meta.StateRoot {
		t.Errorf("StateRoot mismatch: got %s, want %s", info.StateRoot, meta.StateRoot)
	}

	// Verify original fields preserved
	if _, ok := testObj["env"]; !ok {
		t.Error("env key missing from test object")
	}
	if _, ok := testObj["pre"]; !ok {
		t.Error("pre key missing from test object")
	}
}

func TestInjectCrossVMMetadata_RemovesLegacyCrossvm(t *testing.T) {
	// Create test JSON with legacy _crossvm key
	testJSON := []byte(`{
  "_crossvm": {"old": "data"},
  "testName": {
    "env": {},
    "pre": {},
    "transaction": {}
  }
}`)

	meta := &CrossVMMetadata{
		GeneratedBy: "geth",
		TraceHash:   "abc123",
	}

	result, err := InjectCrossVMMetadata(testJSON, meta)
	if err != nil {
		t.Fatalf("InjectCrossVMMetadata failed: %v", err)
	}

	// Parse result
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("Failed to parse result: %v", err)
	}

	// _crossvm should be removed
	if _, ok := parsed["_crossvm"]; ok {
		t.Error("Legacy _crossvm key should have been removed")
	}
}

func TestDetectFork(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected string
	}{
		{
			name: "London fork",
			json: `{"test": {"post": {"London": []}}}`,
			expected: "London",
		},
		{
			name: "Prague fork",
			json: `{"test": {"post": {"Prague": []}}}`,
			expected: "Prague",
		},
		{
			name: "No post section",
			json: `{"test": {"env": {}}}`,
			expected: "",
		},
		{
			name: "Invalid JSON",
			json: `not json`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := detectFork([]byte(tt.json))
			if got != tt.expected {
				t.Errorf("detectFork() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestProcessConfig_Defaults(t *testing.T) {
	config := DefaultProcessConfig()

	if config.Workers <= 0 {
		t.Error("Workers should be > 0")
	}
	if config.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", config.Timeout)
	}
	if config.SkipInvalid != false {
		t.Error("SkipInvalid should default to false")
	}
	if config.DryRun != false {
		t.Error("DryRun should default to false")
	}
}

func TestBatchProcessor_DryRun(t *testing.T) {
	// Create temp input directory with a test file
	inputDir := t.TempDir()
	outputDir := t.TempDir()

	// Write a minimal state test
	testData := GenerateMinimalSeed()
	testFile := filepath.Join(inputDir, "test.json")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	// Create processor with dry-run enabled
	config := ProcessConfig{
		Workers:     1,
		Timeout:     10 * time.Second,
		DryRun:      true,
		Quiet:       true,
		SkipInvalid: true,
	}

	processor, err := NewBatchProcessor(inputDir, outputDir, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Run processor
	if err := processor.Run(); err != nil {
		t.Fatalf("Processor.Run failed: %v", err)
	}

	// Verify no output was written
	outputTestDir := filepath.Join(outputDir, "tests")
	if _, err := os.Stat(outputTestDir); !os.IsNotExist(err) {
		// Check if directory is empty (created but no files)
		entries, _ := os.ReadDir(outputTestDir)
		if len(entries) > 0 {
			t.Error("Dry run should not write any output files")
		}
	}
}

func TestBatchProcessor_InvalidInputDir(t *testing.T) {
	config := DefaultProcessConfig()
	_, err := NewBatchProcessor("/nonexistent/path", "/tmp/out", config)
	if err == nil {
		t.Error("Expected error for nonexistent input directory")
	}
}

func TestBatchProcessor_InputNotDirectory(t *testing.T) {
	// Create a file (not a directory)
	tmpFile := filepath.Join(t.TempDir(), "notadir.txt")
	if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	config := DefaultProcessConfig()
	_, err := NewBatchProcessor(tmpFile, "/tmp/out", config)
	if err == nil {
		t.Error("Expected error when input is a file, not a directory")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("Error message should mention 'not a directory', got: %v", err)
	}
}

func TestBatchProcessor_ProcessFile(t *testing.T) {
	// This test requires actual execution, so skip if running short tests
	if testing.Short() {
		t.Skip("Skipping execution test in short mode")
	}

	inputDir := t.TempDir()
	outputDir := t.TempDir()

	// Write a minimal state test
	testData := GenerateMinimalSeed()
	testFile := filepath.Join(inputDir, "test.json")
	if err := os.WriteFile(testFile, testData, 0644); err != nil {
		t.Fatalf("Failed to write test file: %v", err)
	}

	config := ProcessConfig{
		Workers:     1,
		Timeout:     30 * time.Second,
		Quiet:       true,
		SkipInvalid: true,
	}

	processor, err := NewBatchProcessor(inputDir, outputDir, config)
	if err != nil {
		t.Fatalf("Failed to create processor: %v", err)
	}

	// Run processor
	if err := processor.Run(); err != nil {
		t.Fatalf("Processor.Run failed: %v", err)
	}

	// Verify manifest was written
	manifestPath := filepath.Join(outputDir, "manifest.json")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		t.Error("Manifest file should have been written")
	}

	// Load and verify manifest
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("Failed to load manifest: %v", err)
	}

	if manifest.TotalTests != 1 {
		t.Errorf("TotalTests = %d, want 1", manifest.TotalTests)
	}
	if manifest.GeneratedBy != "geth" {
		t.Errorf("GeneratedBy = %s, want geth", manifest.GeneratedBy)
	}
}

func TestProcessFile_Standalone(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping execution test in short mode")
	}

	// Create temp file
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.json")
	if err := os.WriteFile(testFile, GenerateMinimalSeed(), 0644); err != nil {
		t.Fatal(err)
	}

	result, err := ProcessFile(testFile, 30*time.Second)
	if err != nil {
		t.Fatalf("ProcessFile failed: %v", err)
	}

	if result.TraceHash == "" {
		t.Error("TraceHash should not be empty")
	}
	if result.StateRoot == "" {
		t.Error("StateRoot should not be empty")
	}
	if result.InputPath != testFile {
		t.Errorf("InputPath = %s, want %s", result.InputPath, testFile)
	}
}

func TestProcessFile_InvalidFile(t *testing.T) {
	_, err := ProcessFile("/nonexistent/file.json", time.Second)
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestProcessFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "invalid.json")
	if err := os.WriteFile(testFile, []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ProcessFile(testFile, time.Second)
	if err == nil {
		t.Error("Expected error for invalid JSON")
	}
}
