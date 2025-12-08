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
	"time"
)

// FuzzerMetadata contains cross-client verification data
type FuzzerMetadata struct {
	TraceHash      string  `json:"traceHash"`        // MD5 hash of normalized geth trace
	StateRoot      string  `json:"stateRoot"`        // Expected post-state root
	TraceLines     int     `json:"traceLines"`       // Number of trace lines
	GasUsed        uint64  `json:"gasUsed"`          // Gas consumed
	GethVersion    string  `json:"gethVersion"`      // Geth version used
	GeneratedAt    string  `json:"generatedAt"`      // ISO timestamp
	CoverageDelta  float64 `json:"coverageDelta"`    // Coverage improvement
	MutationStrat  string  `json:"mutationStrategy"` // Which strategy created this
}

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

// SaveEnhancedCorpusEntry saves a state test with embedded trace hash
func (cs *CorpusSaver) SaveEnhancedCorpusEntry(
	testJSON []byte,
	traceResult *TracingResult,
	coverageDelta float64,
	strategyName string,
) (string, error) {
	// Parse original test
	var original map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &original); err != nil {
		return "", fmt.Errorf("failed to parse test JSON: %w", err)
	}

	// Add fuzzer metadata
	meta := &FuzzerMetadata{
		TraceHash:     traceResult.TraceHash,
		StateRoot:     traceResult.StateRoot,
		TraceLines:    traceResult.TraceLines,
		GasUsed:       traceResult.GasUsed,
		GethVersion:   cs.gethVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		CoverageDelta: coverageDelta,
		MutationStrat: strategyName,
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("failed to marshal metadata: %w", err)
	}
	original["_fuzzer"] = metaJSON

	// Generate filename from trace hash (first 16 chars for uniqueness)
	filename := fmt.Sprintf("%s.json", traceResult.TraceHash[:16])
	fullPath := filepath.Join(cs.corpusDir, filename)

	// Check if file already exists (duplicate trace hash)
	if _, err := os.Stat(fullPath); err == nil {
		// File exists, append a counter
		for i := 1; i < 1000; i++ {
			filename = fmt.Sprintf("%s_%d.json", traceResult.TraceHash[:16], i)
			fullPath = filepath.Join(cs.corpusDir, filename)
			if _, err := os.Stat(fullPath); os.IsNotExist(err) {
				break
			}
		}
	}

	// Write with pretty printing
	output, err := json.MarshalIndent(original, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal enhanced test: %w", err)
	}

	if err := os.WriteFile(fullPath, output, 0644); err != nil {
		return "", fmt.Errorf("failed to write corpus file: %w", err)
	}

	cs.savedCount++
	return fullPath, nil
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

// LoadEnhancedCorpus loads corpus entries with their fuzzer metadata
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

		// Extract fuzzer metadata if present
		if fuzzerRaw, ok := raw["_fuzzer"]; ok {
			var meta FuzzerMetadata
			if err := json.Unmarshal(fuzzerRaw, &meta); err == nil {
				entry.Metadata = &meta
			}
		}

		entries = append(entries, entry)
		return nil
	})

	return entries, err
}

// EnhancedCorpusEntry represents a corpus entry with optional metadata
type EnhancedCorpusEntry struct {
	Path     string          // File path
	TestRaw  []byte          // Raw test JSON
	Metadata *FuzzerMetadata // Optional fuzzer metadata
}

// HasMetadata returns true if this entry has fuzzer metadata
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

// VerifyAgainstTrace verifies if a new trace matches the expected trace hash
func (e *EnhancedCorpusEntry) VerifyAgainstTrace(newTraceHash string) bool {
	if e.Metadata == nil {
		return false // Can't verify without metadata
	}
	return e.Metadata.TraceHash == newTraceHash
}

// StripFuzzerMetadata returns the test JSON without fuzzer metadata
func StripFuzzerMetadata(testJSON []byte) ([]byte, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &raw); err != nil {
		return nil, err
	}

	delete(raw, "_fuzzer")

	return json.Marshal(raw)
}
