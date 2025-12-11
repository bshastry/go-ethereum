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
	"encoding/csv"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MetricsExporter writes longitudinal fuzzing metrics to CSV files for post-run analysis.
// It maintains two CSV files:
//   - {basePath}_main.csv: Overall fuzzing metrics per record interval
//   - {basePath}_sources.csv: Per-source (strategy/generator) breakdown
//
// The exporter is thread-safe and uses buffered writes to reduce I/O syscalls.
type MetricsExporter struct {
	basePath string

	mainFile      *os.File
	mainWriter    *csv.Writer
	sourcesFile   *os.File
	sourcesWriter *csv.Writer

	mu          sync.Mutex
	startTime   time.Time
	recordCount int
	closed      bool
}

// mainCSVHeader defines the columns for the main metrics CSV file.
var mainCSVHeader = []string{
	"timestamp",
	"elapsed_sec",
	"provider",
	"coverage_pct",
	"exec_total",
	"exec_per_sec",
	"coverage_finds",
	"crashes",
	"timeouts",
	"hp_queue_len",
	"hp_picks",
	"seed_picks",
	"total_culled",
	"total_added",
	"effective_hp_prob",
	"splicing_len",
	"mutation_finds_total",
	"generation_finds_total",
}

// sourcesCSVHeader defines the columns for the sources breakdown CSV file.
var sourcesCSVHeader = []string{
	"timestamp",
	"source_type",
	"source_name",
	"inputs",
	"finds",
	"find_rate",
	"total_delta",
}

// flushInterval controls how often buffered records are flushed to disk.
const flushInterval = 10

// NewMetricsExporter creates a new metrics exporter that writes to files
// with the given base path. Creates two files:
//   - {basePath}_main.csv
//   - {basePath}_sources.csv
//
// Returns an error if the files cannot be created.
func NewMetricsExporter(basePath string) (*MetricsExporter, error) {
	mainPath := basePath + "_main.csv"
	sourcesPath := basePath + "_sources.csv"

	mainFile, err := os.Create(mainPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create main CSV file: %w", err)
	}

	sourcesFile, err := os.Create(sourcesPath)
	if err != nil {
		mainFile.Close()
		return nil, fmt.Errorf("failed to create sources CSV file: %w", err)
	}

	mainWriter := csv.NewWriter(mainFile)
	sourcesWriter := csv.NewWriter(sourcesFile)

	// Write headers
	if err := mainWriter.Write(mainCSVHeader); err != nil {
		mainFile.Close()
		sourcesFile.Close()
		return nil, fmt.Errorf("failed to write main CSV header: %w", err)
	}

	if err := sourcesWriter.Write(sourcesCSVHeader); err != nil {
		mainFile.Close()
		sourcesFile.Close()
		return nil, fmt.Errorf("failed to write sources CSV header: %w", err)
	}

	// Flush headers immediately
	mainWriter.Flush()
	if err := mainWriter.Error(); err != nil {
		mainFile.Close()
		sourcesFile.Close()
		return nil, fmt.Errorf("failed to flush main CSV header: %w", err)
	}

	sourcesWriter.Flush()
	if err := sourcesWriter.Error(); err != nil {
		mainFile.Close()
		sourcesFile.Close()
		return nil, fmt.Errorf("failed to flush sources CSV header: %w", err)
	}

	return &MetricsExporter{
		basePath:      basePath,
		mainFile:      mainFile,
		mainWriter:    mainWriter,
		sourcesFile:   sourcesFile,
		sourcesWriter: sourcesWriter,
		startTime:     time.Now(),
		recordCount:   0,
		closed:        false,
	}, nil
}

// Record writes a metrics snapshot to the CSV files.
// It extracts relevant statistics from the provided FuzzStats, InputProvider, and CoverageCorpus.
//
// Parameters:
//   - stats: FuzzStats containing execution counts, crashes, timeouts, and coverage finds
//   - provider: InputProvider for source breakdown (can be nil for mutation-only mode)
//   - corpus: CoverageCorpus for queue and priority statistics
//
// Thread-safe: uses mutex to protect concurrent writes.
// Returns an error if the exporter has been closed or if writing fails.
func (m *MetricsExporter) Record(stats *FuzzStats, provider InputProvider, corpus *CoverageCorpus) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return fmt.Errorf("metrics exporter is closed")
	}

	now := time.Now()
	timestamp := now.Unix()
	elapsed := now.Sub(m.startTime).Seconds()

	// Extract basic stats using atomic loads
	execTotal := atomic.LoadInt64(&stats.totalExecs)
	crashes := atomic.LoadInt64(&stats.totalCrashes)
	timeouts := atomic.LoadInt64(&stats.totalTimeouts)
	coverageFinds := atomic.LoadInt64(&stats.coverageFinds)

	// Calculate execution rate
	var execPerSec float64
	if elapsed > 0 {
		execPerSec = float64(execTotal) / elapsed
	}

	// Get coverage percentage
	coveragePct := testing.Coverage() * 100

	// Get corpus stats
	var corpusStats CoverageCorpusStats
	if corpus != nil {
		corpusStats = corpus.FullStats()
	}

	// Determine provider name and extract source breakdown
	providerName := "mutation"
	var providerStats ProviderStats
	if provider != nil {
		providerName = provider.Name()
		providerStats = provider.Stats()
	}

	// Calculate mutation vs generation totals
	var mutFindsTotal, genFindsTotal int64
	if provider == nil {
		// Mutation-only mode: all finds are from mutation
		mutFindsTotal = coverageFinds
		genFindsTotal = 0
	} else {
		// Extract from source breakdown if available
		if mutStats, ok := providerStats.SourceBreakdown["[mutation_total]"]; ok {
			mutFindsTotal = mutStats.CoverageFinds
		}
		if genStats, ok := providerStats.SourceBreakdown["[generation_total]"]; ok {
			genFindsTotal = genStats.CoverageFinds
		}
	}

	// Write main record
	mainRecord := []string{
		fmt.Sprintf("%d", timestamp),
		fmt.Sprintf("%.2f", elapsed),
		providerName,
		fmt.Sprintf("%.6f", coveragePct),
		fmt.Sprintf("%d", execTotal),
		fmt.Sprintf("%.2f", execPerSec),
		fmt.Sprintf("%d", coverageFinds),
		fmt.Sprintf("%d", crashes),
		fmt.Sprintf("%d", timeouts),
		fmt.Sprintf("%d", corpusStats.HPQueueLen),
		fmt.Sprintf("%d", corpusStats.HPPicks),
		fmt.Sprintf("%d", corpusStats.SeedPicks),
		fmt.Sprintf("%d", corpusStats.TotalCulled),
		fmt.Sprintf("%d", corpusStats.TotalAdded),
		fmt.Sprintf("%.4f", corpusStats.EffectiveP),
		fmt.Sprintf("%d", corpusStats.SplicingLen),
		fmt.Sprintf("%d", mutFindsTotal),
		fmt.Sprintf("%d", genFindsTotal),
	}

	if err := m.mainWriter.Write(mainRecord); err != nil {
		return fmt.Errorf("failed to write main record: %w", err)
	}

	// Write source breakdown records
	if providerStats.SourceBreakdown != nil {
		for sourceKey, sourceStats := range providerStats.SourceBreakdown {
			// Skip aggregate keys (keys starting with '[')
			if len(sourceKey) > 0 && sourceKey[0] == '[' {
				continue
			}

			// Skip sources with 0 inputs
			if sourceStats.Inputs == 0 {
				continue
			}

			// Parse source key into type and name
			// Format: "mutation:bytecode" -> type="mutation", name="bytecode"
			sourceType, sourceName := parseSourceKey(sourceKey)

			sourceRecord := []string{
				fmt.Sprintf("%d", timestamp),
				sourceType,
				sourceName,
				fmt.Sprintf("%d", sourceStats.Inputs),
				fmt.Sprintf("%d", sourceStats.CoverageFinds),
				fmt.Sprintf("%.6f", sourceStats.FindRate()),
				fmt.Sprintf("%.6f", sourceStats.TotalDelta),
			}

			if err := m.sourcesWriter.Write(sourceRecord); err != nil {
				return fmt.Errorf("failed to write source record: %w", err)
			}
		}
	}

	m.recordCount++

	// Buffered flushing: flush every flushInterval records
	if m.recordCount%flushInterval == 0 {
		m.mainWriter.Flush()
		if err := m.mainWriter.Error(); err != nil {
			return fmt.Errorf("failed to flush main CSV: %w", err)
		}

		m.sourcesWriter.Flush()
		if err := m.sourcesWriter.Error(); err != nil {
			return fmt.Errorf("failed to flush sources CSV: %w", err)
		}
	}

	return nil
}

// Close flushes any remaining buffered data and closes the CSV files.
// After Close is called, Record will return an error.
// It is safe to call Close multiple times.
func (m *MetricsExporter) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	var errs []error

	// Flush remaining data
	m.mainWriter.Flush()
	if err := m.mainWriter.Error(); err != nil {
		errs = append(errs, fmt.Errorf("main CSV flush error: %w", err))
	}

	m.sourcesWriter.Flush()
	if err := m.sourcesWriter.Error(); err != nil {
		errs = append(errs, fmt.Errorf("sources CSV flush error: %w", err))
	}

	// Close files
	if err := m.mainFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("main CSV close error: %w", err))
	}

	if err := m.sourcesFile.Close(); err != nil {
		errs = append(errs, fmt.Errorf("sources CSV close error: %w", err))
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// BasePath returns the base path used for the CSV files.
func (m *MetricsExporter) BasePath() string {
	return m.basePath
}

// parseSourceKey splits a source key like "mutation:bytecode" into type and name.
// If no colon is found, returns the key as the name with type "unknown".
func parseSourceKey(key string) (sourceType, sourceName string) {
	idx := strings.Index(key, ":")
	if idx < 0 {
		return "unknown", key
	}
	return key[:idx], key[idx+1:]
}
