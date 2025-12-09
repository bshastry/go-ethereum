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
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
)

// CrossVMMetadata contains cross-client verification data following the EEST standard.
// This format is designed for 6-VM differential testing:
// geth, nethermind, besu, erigon, revm, evmone
//
// The metadata is stored inside the "_info" field of each test object, following
// the EEST (Ethereum Execution Spec Tests) standard format. Example:
//
//	{
//	  "testName": {
//	    "_info": {
//	      "comment": "Cross-VM consensus verification test",
//	      "generatedBy": "geth",
//	      "traceHash": "abc123...",
//	      ...
//	    },
//	    "env": {...},
//	    "pre": {...},
//	    ...
//	  }
//	}
//
// When a VM loads a corpus entry with _info metadata from another VM,
// it should execute the test without mutation and verify the trace hash matches.
type CrossVMMetadata struct {
	// Required fields
	Comment        string `json:"comment,omitempty"`        // Human-readable description
	GeneratedBy    string `json:"generatedBy"`              // Which VM generated this (geth, nethermind, besu, erigon, revm, evmone)
	TraceHash      string `json:"traceHash"`                // MD5 hash of normalized trace + stateRoot (cross-VM comparable)
	StateRoot      string `json:"stateRoot"`                // Expected post-state root (for quick pre-check)
	CrossVMVersion string `json:"crossvmVersion"`           // Metadata schema version (currently "1.0")
	// Optional fields
	TraceLines  int    `json:"traceLines,omitempty"`  // Number of trace lines
	GeneratedAt string `json:"generatedAt,omitempty"` // ISO timestamp
	Version     string `json:"version,omitempty"`     // VM version
	Fork        string `json:"fork,omitempty"`        // Fork name (e.g., "Cancun", "Prague")
	// EEST standard field (for compatibility with standard test format)
	FixtureFormat string `json:"fixture-format,omitempty"` // Fixture format identifier
}

// CrossVMMetadataVersion is the current version of the cross-VM metadata schema
const CrossVMMetadataVersion = "1.0"

// CrossVMDefaultComment is the default comment for cross-VM generated tests
const CrossVMDefaultComment = "Cross-VM consensus verification test"


// CorpusSaver handles saving coverage-finding inputs with trace metadata
type CorpusSaver struct {
	corpusDir   string
	gethVersion string
	savedCount  int
}

// NewCorpusSaver creates a new corpus saver
func NewCorpusSaver(corpusDir string) (*CorpusSaver, error) {
	if err := os.MkdirAll(corpusDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create corpus directory: %w", err)
	}

	return &CorpusSaver{
		corpusDir:   corpusDir,
		gethVersion: getGethVersion(),
	}, nil
}

// SaveEnhancedCorpusEntry saves a state test with embedded cross-VM metadata.
// The metadata is injected into the "_info" field inside each test object,
// following the EEST (Ethereum Execution Spec Tests) standard format.
func (cs *CorpusSaver) SaveEnhancedCorpusEntry(
	testJSON []byte,
	traceResult *TracingResult,
	coverageDelta float64,
	strategyName string,
) (string, error) {
	// Parse original test as map of test names to raw test objects
	var original map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &original); err != nil {
		return "", fmt.Errorf("failed to parse test JSON: %w", err)
	}

	// Build cross-VM metadata following EEST standard
	meta := &CrossVMMetadata{
		Comment:        CrossVMDefaultComment,
		GeneratedBy:    "geth",
		TraceHash:      traceResult.TraceHash,
		StateRoot:      traceResult.StateRoot,
		CrossVMVersion: CrossVMMetadataVersion,
		TraceLines:     traceResult.TraceLines,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Version:        cs.gethVersion,
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Inject _info into each test object (EEST standard placement)
	for testName, testRaw := range original {
		// Skip any legacy metadata keys at top level
		if testName == "_crossvm" || testName == "_info" {
			continue
		}

		// Parse the test object
		var testObj map[string]json.RawMessage
		if err := json.Unmarshal(testRaw, &testObj); err != nil {
			// Not a valid test object, skip
			continue
		}

		// Inject _info into the test object
		testObj["_info"] = metaJSON

		// Re-marshal the test object
		updatedTestRaw, err := json.Marshal(testObj)
		if err != nil {
			return "", fmt.Errorf("failed to re-marshal test object %s: %w", testName, err)
		}
		original[testName] = updatedTestRaw
	}

	// Remove any legacy top-level _crossvm key (cleanup old format)
	delete(original, "_crossvm")

	// Write with pretty printing
	output, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal enhanced test: %w", err)
	}

	// Generate filename from trace hash (first 16 chars for uniqueness).
	// Use O_EXCL for atomic file creation to avoid race conditions when
	// multiple workers produce the same trace hash concurrently.
	baseFilename := traceResult.TraceHash[:16]

	// Try to create file atomically with O_EXCL (fails if exists)
	for i := 0; i < 10000; i++ {
		var filename string
		if i == 0 {
			filename = baseFilename + ".json"
		} else {
			filename = fmt.Sprintf("%s_%d.json", baseFilename, i)
		}
		fullPath := filepath.Join(cs.corpusDir, filename)

		// O_EXCL ensures atomic create - fails if file exists
		f, err := os.OpenFile(fullPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if os.IsExist(err) {
				continue // File exists, try next counter
			}
			return "", fmt.Errorf("failed to create corpus file: %w", err)
		}
		// Successfully created file exclusively
		_, writeErr := f.Write(output)
		closeErr := f.Close()
		if writeErr != nil {
			return "", fmt.Errorf("failed to write corpus file: %w", writeErr)
		}
		if closeErr != nil {
			return "", fmt.Errorf("failed to close corpus file: %w", closeErr)
		}
		cs.savedCount++
		return fullPath, nil
	}

	return "", fmt.Errorf("failed to find unique filename after 10000 attempts for hash %s", baseFilename)
}

// SavedCount returns the number of corpus entries saved
func (cs *CorpusSaver) SavedCount() int {
	return cs.savedCount
}

// CorpusDir returns the corpus directory path
func (cs *CorpusSaver) CorpusDir() string {
	return cs.corpusDir
}

// getGethVersion returns the geth version from build info
func getGethVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	// Look for the main module version
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}

	// Try to find git revision from build settings
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			if len(setting.Value) > 8 {
				return setting.Value[:8]
			}
			return setting.Value
		}
	}

	return "dev"
}

// LoadEnhancedCorpus loads corpus entries with their cross-VM metadata.
// It supports both the new EEST standard format (metadata in _info inside test objects)
// and the legacy format (metadata in top-level _crossvm) for backward compatibility.
func LoadEnhancedCorpus(corpusDir string) ([]EnhancedCorpusEntry, error) {
	var entries []EnhancedCorpusEntry

	err := filepath.Walk(corpusDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil // Skip unreadable files
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil // Skip invalid JSON
		}

		entry := EnhancedCorpusEntry{
			Path:    path,
			TestRaw: data,
		}

		// Try to extract cross-VM metadata using the helper function
		entry.Metadata = extractCrossVMMetadataFromRaw(raw)

		entries = append(entries, entry)
		return nil
	})

	return entries, err
}

// extractCrossVMMetadataFromRaw extracts cross-VM metadata from parsed JSON.
// It supports both EEST standard format (_info inside test objects) and
// legacy format (top-level _crossvm) for backward compatibility.
func extractCrossVMMetadataFromRaw(raw map[string]json.RawMessage) *CrossVMMetadata {
	// Try EEST standard format first: _info inside each test object
	for testName, testRaw := range raw {
		// Skip metadata keys
		if testName == "_crossvm" || testName == "_info" {
			continue
		}

		var testObj map[string]json.RawMessage
		if err := json.Unmarshal(testRaw, &testObj); err != nil {
			continue
		}

		if infoRaw, ok := testObj["_info"]; ok {
			var meta CrossVMMetadata
			if err := json.Unmarshal(infoRaw, &meta); err == nil {
				// Validate that this is actually cross-VM metadata (has required fields)
				if meta.GeneratedBy != "" && meta.TraceHash != "" {
					return &meta
				}
			}
		}
	}

	// Fallback to legacy format: top-level _crossvm
	if crossvmRaw, ok := raw["_crossvm"]; ok {
		var meta CrossVMMetadata
		if err := json.Unmarshal(crossvmRaw, &meta); err == nil {
			return &meta
		}
	}

	return nil
}

// EnhancedCorpusEntry represents a corpus entry with optional cross-VM metadata
type EnhancedCorpusEntry struct {
	Path     string           // File path
	TestRaw  []byte           // Raw test JSON
	Metadata *CrossVMMetadata // Optional cross-VM metadata
}

// HasMetadata returns true if this entry has cross-VM metadata
func (e *EnhancedCorpusEntry) HasMetadata() bool {
	return e.Metadata != nil
}

// GetTraceHash returns the trace hash if available
func (e *EnhancedCorpusEntry) GetTraceHash() string {
	if e.Metadata != nil {
		return e.Metadata.TraceHash
	}
	return ""
}

// GetGeneratedBy returns which VM generated this entry
func (e *EnhancedCorpusEntry) GetGeneratedBy() string {
	if e.Metadata != nil {
		return e.Metadata.GeneratedBy
	}
	return ""
}

// IsCrossVMEntry returns true if this entry was generated by a different VM.
// Comparison is case-insensitive for compatibility with other client implementations.
func (e *EnhancedCorpusEntry) IsCrossVMEntry(currentVM string) bool {
	if e.Metadata == nil {
		return false
	}
	return e.Metadata.GeneratedBy != "" &&
		!strings.EqualFold(e.Metadata.GeneratedBy, currentVM)
}

// VerifyAgainstTrace verifies if a new trace matches the expected trace hash
func (e *EnhancedCorpusEntry) VerifyAgainstTrace(newTraceHash string) bool {
	if e.Metadata == nil {
		return false // Can't verify without metadata
	}
	return e.Metadata.TraceHash == newTraceHash
}

// StripCrossVMMetadata returns the test JSON without cross-VM metadata.
// It removes both the EEST standard format (_info inside test objects) and
// the legacy format (top-level _crossvm) for compatibility.
func StripCrossVMMetadata(testJSON []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &raw); err != nil {
		return nil, err
	}

	// Remove legacy top-level _crossvm if present
	delete(raw, "_crossvm")

	// Remove _info from inside each test object (EEST standard format)
	for testName, testRaw := range raw {
		// Skip metadata keys
		if testName == "_crossvm" || testName == "_info" {
			continue
		}

		var testObj map[string]json.RawMessage
		if err := json.Unmarshal(testRaw, &testObj); err != nil {
			continue // Not a valid test object, skip
		}

		// Check if _info exists and contains cross-VM metadata
		if infoRaw, ok := testObj["_info"]; ok {
			var meta CrossVMMetadata
			if err := json.Unmarshal(infoRaw, &meta); err == nil {
				// Only remove if it's actually cross-VM metadata
				if meta.GeneratedBy != "" || meta.TraceHash != "" {
					delete(testObj, "_info")
					if updated, err := json.Marshal(testObj); err == nil {
						raw[testName] = updated
					}
				}
			}
		}
	}

	return json.Marshal(raw)
}

