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
	"runtime/debug"
	"time"
)

// Manifest describes a processed corpus
type Manifest struct {
	Version         string         `json:"version"`
	GeneratedAt     time.Time      `json:"generatedAt"`
	GeneratedBy     string         `json:"generatedBy"`
	GethVersion     string         `json:"gethVersion"`
	TotalTests      int            `json:"totalTests"`
	SuccessfulTests int            `json:"successfulTests"`
	FailedTests     int            `json:"failedTests"`
	SkippedTests    int            `json:"skippedTests"`
	ForkCounts      map[string]int `json:"forks,omitempty"`
	ProcessingTime  time.Duration  `json:"processingTime"`
	Workers         int            `json:"workers"`
}

// manifestJSON is used for JSON serialization with custom duration handling
type manifestJSON struct {
	Version         string         `json:"version"`
	GeneratedAt     string         `json:"generatedAt"`
	GeneratedBy     string         `json:"generatedBy"`
	GethVersion     string         `json:"gethVersion"`
	TotalTests      int            `json:"totalTests"`
	SuccessfulTests int            `json:"successfulTests"`
	FailedTests     int            `json:"failedTests"`
	SkippedTests    int            `json:"skippedTests"`
	ForkCounts      map[string]int `json:"forks,omitempty"`
	ProcessingTime  string         `json:"processingTime"`
	Workers         int            `json:"workers"`
}

// NewManifest creates a new manifest with default values
func NewManifest() *Manifest {
	return &Manifest{
		Version:     "1.0",
		GeneratedBy: "geth",
		GethVersion: getGethVersionForManifest(),
		ForkCounts:  make(map[string]int),
	}
}

// WriteToFile writes the manifest to a JSON file
func (m *Manifest) WriteToFile(path string) error {
	// Convert to JSON-friendly format
	mj := manifestJSON{
		Version:         m.Version,
		GeneratedAt:     m.GeneratedAt.Format(time.RFC3339),
		GeneratedBy:     m.GeneratedBy,
		GethVersion:     m.GethVersion,
		TotalTests:      m.TotalTests,
		SuccessfulTests: m.SuccessfulTests,
		FailedTests:     m.FailedTests,
		SkippedTests:    m.SkippedTests,
		ForkCounts:      m.ForkCounts,
		ProcessingTime:  m.ProcessingTime.String(),
		Workers:         m.Workers,
	}

	data, err := json.MarshalIndent(mj, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// LoadManifest loads a manifest from a JSON file
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var mj manifestJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return nil, err
	}

	// Parse timestamp
	generatedAt, err := time.Parse(time.RFC3339, mj.GeneratedAt)
	if err != nil {
		generatedAt = time.Time{}
	}

	// Parse duration
	processingTime, err := time.ParseDuration(mj.ProcessingTime)
	if err != nil {
		processingTime = 0
	}

	return &Manifest{
		Version:         mj.Version,
		GeneratedAt:     generatedAt,
		GeneratedBy:     mj.GeneratedBy,
		GethVersion:     mj.GethVersion,
		TotalTests:      mj.TotalTests,
		SuccessfulTests: mj.SuccessfulTests,
		FailedTests:     mj.FailedTests,
		SkippedTests:    mj.SkippedTests,
		ForkCounts:      mj.ForkCounts,
		ProcessingTime:  processingTime,
		Workers:         mj.Workers,
	}, nil
}

// MarshalJSON implements json.Marshaler for Manifest
func (m *Manifest) MarshalJSON() ([]byte, error) {
	mj := manifestJSON{
		Version:         m.Version,
		GeneratedAt:     m.GeneratedAt.Format(time.RFC3339),
		GeneratedBy:     m.GeneratedBy,
		GethVersion:     m.GethVersion,
		TotalTests:      m.TotalTests,
		SuccessfulTests: m.SuccessfulTests,
		FailedTests:     m.FailedTests,
		SkippedTests:    m.SkippedTests,
		ForkCounts:      m.ForkCounts,
		ProcessingTime:  m.ProcessingTime.String(),
		Workers:         m.Workers,
	}
	return json.Marshal(mj)
}

// UnmarshalJSON implements json.Unmarshaler for Manifest
func (m *Manifest) UnmarshalJSON(data []byte) error {
	var mj manifestJSON
	if err := json.Unmarshal(data, &mj); err != nil {
		return err
	}

	m.Version = mj.Version
	m.GeneratedBy = mj.GeneratedBy
	m.GethVersion = mj.GethVersion
	m.TotalTests = mj.TotalTests
	m.SuccessfulTests = mj.SuccessfulTests
	m.FailedTests = mj.FailedTests
	m.SkippedTests = mj.SkippedTests
	m.ForkCounts = mj.ForkCounts
	m.Workers = mj.Workers

	// Parse timestamp
	if generatedAt, err := time.Parse(time.RFC3339, mj.GeneratedAt); err == nil {
		m.GeneratedAt = generatedAt
	}

	// Parse duration
	if processingTime, err := time.ParseDuration(mj.ProcessingTime); err == nil {
		m.ProcessingTime = processingTime
	}

	return nil
}

// getGethVersionForManifest returns the geth version from build info
func getGethVersionForManifest() string {
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

// ErrorLog handles error logging to a file
type ErrorLog struct {
	file   *os.File
	dryRun bool
}

// NewErrorLog creates a new error log
func NewErrorLog(path string, dryRun bool) *ErrorLog {
	if dryRun {
		return &ErrorLog{dryRun: true}
	}

	f, err := os.Create(path)
	if err != nil {
		// If we can't create the error log, continue without it
		return &ErrorLog{dryRun: true}
	}
	return &ErrorLog{file: f}
}

// Log logs an error to the file
func (l *ErrorLog) Log(path string, err error) {
	if l.dryRun || l.file == nil {
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] ERROR: %s: %v\n", timestamp, path, err)
	l.file.WriteString(line)
}

// LogWarning logs a warning to the file
func (l *ErrorLog) LogWarning(path string, message string) {
	if l.dryRun || l.file == nil {
		return
	}
	timestamp := time.Now().UTC().Format(time.RFC3339)
	line := fmt.Sprintf("[%s] WARN:  %s: %s\n", timestamp, path, message)
	l.file.WriteString(line)
}

// Close closes the error log file
func (l *ErrorLog) Close() {
	if l.file != nil {
		l.file.Close()
	}
}

// CorpusStats contains statistics about a corpus
type CorpusStats struct {
	TotalFiles    int            `json:"totalFiles"`
	ValidTests    int            `json:"validTests"`
	InvalidTests  int            `json:"invalidTests"`
	WithMetadata  int            `json:"withMetadata"`
	ForkCounts    map[string]int `json:"forkCounts"`
	MinTraceLines int            `json:"minTraceLines"`
	MaxTraceLines int            `json:"maxTraceLines"`
	AvgTraceLines float64        `json:"avgTraceLines"`
	OldestFile    time.Time      `json:"oldestFile,omitempty"`
	NewestFile    time.Time      `json:"newestFile,omitempty"`
}

// NewCorpusStats creates a new empty stats structure
func NewCorpusStats() *CorpusStats {
	return &CorpusStats{
		ForkCounts: make(map[string]int),
	}
}

// GatherCorpusStats walks a corpus directory and gathers statistics
func GatherCorpusStats(corpusDir string) (*CorpusStats, error) {
	stats := NewCorpusStats()

	entries, err := LoadEnhancedCorpus(corpusDir)
	if err != nil {
		return nil, fmt.Errorf("load corpus: %w", err)
	}

	totalTraceLines := 0

	for _, entry := range entries {
		stats.TotalFiles++

		if !isValidStateTestJSON(entry.TestRaw) {
			stats.InvalidTests++
			continue
		}
		stats.ValidTests++

		if entry.HasMetadata() {
			stats.WithMetadata++

			meta := entry.Metadata
			traceLines := meta.TraceLines

			// Update trace line stats
			totalTraceLines += traceLines
			if stats.MinTraceLines == 0 || traceLines < stats.MinTraceLines {
				stats.MinTraceLines = traceLines
			}
			if traceLines > stats.MaxTraceLines {
				stats.MaxTraceLines = traceLines
			}

			// Count forks
			if meta.Fork != "" {
				stats.ForkCounts[meta.Fork]++
			}

			// Track timestamps
			if meta.GeneratedAt != "" {
				if t, err := time.Parse(time.RFC3339, meta.GeneratedAt); err == nil {
					if stats.OldestFile.IsZero() || t.Before(stats.OldestFile) {
						stats.OldestFile = t
					}
					if stats.NewestFile.IsZero() || t.After(stats.NewestFile) {
						stats.NewestFile = t
					}
				}
			}
		}
	}

	// Calculate average
	if stats.WithMetadata > 0 {
		stats.AvgTraceLines = float64(totalTraceLines) / float64(stats.WithMetadata)
	}

	return stats, nil
}
