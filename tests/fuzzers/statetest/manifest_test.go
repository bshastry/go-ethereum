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
	"testing"
	"time"
)

func TestManifest_NewManifest(t *testing.T) {
	m := NewManifest()

	if m.Version != "1.0" {
		t.Errorf("Version = %s, want 1.0", m.Version)
	}
	if m.GeneratedBy != "geth" {
		t.Errorf("GeneratedBy = %s, want geth", m.GeneratedBy)
	}
	if m.ForkCounts == nil {
		t.Error("ForkCounts should be initialized")
	}
}

func TestManifest_WriteAndLoad(t *testing.T) {
	tmpDir := t.TempDir()
	manifestPath := filepath.Join(tmpDir, "manifest.json")

	// Create a manifest with data
	m := NewManifest()
	m.GeneratedAt = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m.TotalTests = 100
	m.SuccessfulTests = 95
	m.FailedTests = 3
	m.SkippedTests = 2
	m.ProcessingTime = 5 * time.Minute
	m.Workers = 8
	m.ForkCounts["London"] = 50
	m.ForkCounts["Prague"] = 45

	// Write to file
	if err := m.WriteToFile(manifestPath); err != nil {
		t.Fatalf("WriteToFile failed: %v", err)
	}

	// Load from file
	loaded, err := LoadManifest(manifestPath)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}

	// Verify fields
	if loaded.Version != m.Version {
		t.Errorf("Version = %s, want %s", loaded.Version, m.Version)
	}
	if loaded.TotalTests != m.TotalTests {
		t.Errorf("TotalTests = %d, want %d", loaded.TotalTests, m.TotalTests)
	}
	if loaded.SuccessfulTests != m.SuccessfulTests {
		t.Errorf("SuccessfulTests = %d, want %d", loaded.SuccessfulTests, m.SuccessfulTests)
	}
	if loaded.FailedTests != m.FailedTests {
		t.Errorf("FailedTests = %d, want %d", loaded.FailedTests, m.FailedTests)
	}
	if loaded.SkippedTests != m.SkippedTests {
		t.Errorf("SkippedTests = %d, want %d", loaded.SkippedTests, m.SkippedTests)
	}
	if loaded.Workers != m.Workers {
		t.Errorf("Workers = %d, want %d", loaded.Workers, m.Workers)
	}
	if loaded.ProcessingTime != m.ProcessingTime {
		t.Errorf("ProcessingTime = %v, want %v", loaded.ProcessingTime, m.ProcessingTime)
	}
	if loaded.ForkCounts["London"] != 50 {
		t.Errorf("ForkCounts[London] = %d, want 50", loaded.ForkCounts["London"])
	}
	if loaded.ForkCounts["Prague"] != 45 {
		t.Errorf("ForkCounts[Prague] = %d, want 45", loaded.ForkCounts["Prague"])
	}

	// Verify timestamp (within 1 second tolerance due to parsing)
	if !loaded.GeneratedAt.Equal(m.GeneratedAt) {
		t.Errorf("GeneratedAt = %v, want %v", loaded.GeneratedAt, m.GeneratedAt)
	}
}

func TestManifest_JSONMarshaling(t *testing.T) {
	m := NewManifest()
	m.GeneratedAt = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	m.ProcessingTime = 2*time.Minute + 30*time.Second
	m.TotalTests = 42

	// Marshal
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	// Unmarshal
	var loaded Manifest
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Verify
	if loaded.TotalTests != m.TotalTests {
		t.Errorf("TotalTests = %d, want %d", loaded.TotalTests, m.TotalTests)
	}
	if loaded.ProcessingTime != m.ProcessingTime {
		t.Errorf("ProcessingTime = %v, want %v", loaded.ProcessingTime, m.ProcessingTime)
	}
}

func TestManifest_LoadNonexistent(t *testing.T) {
	_, err := LoadManifest("/nonexistent/manifest.json")
	if err == nil {
		t.Error("Expected error for nonexistent file")
	}
}

func TestErrorLog_BasicFunctionality(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "errors.log")

	log := NewErrorLog(logPath, false)

	// Log some errors
	log.Log("/path/to/file1.json", os.ErrNotExist)
	log.LogWarning("/path/to/file2.json", "unsupported fork")

	log.Close()

	// Read and verify
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	content := string(data)
	if len(content) == 0 {
		t.Error("Log file should not be empty")
	}

	// Check that errors were logged
	if !contains(content, "ERROR") {
		t.Error("Log should contain ERROR entries")
	}
	if !contains(content, "WARN") {
		t.Error("Log should contain WARN entries")
	}
	if !contains(content, "file1.json") {
		t.Error("Log should contain file paths")
	}
}

func TestErrorLog_DryRun(t *testing.T) {
	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "errors.log")

	// Create log with dryRun=true
	log := NewErrorLog(logPath, true)
	log.Log("/path/to/file.json", os.ErrNotExist)
	log.Close()

	// File should not exist or be empty
	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		data, _ := os.ReadFile(logPath)
		if len(data) > 0 {
			t.Error("DryRun should not write to log file")
		}
	}
}

func TestCorpusStats_New(t *testing.T) {
	stats := NewCorpusStats()

	if stats.ForkCounts == nil {
		t.Error("ForkCounts should be initialized")
	}
	if stats.TotalFiles != 0 {
		t.Error("TotalFiles should be 0")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
