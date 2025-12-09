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
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ProcessConfig configures the batch processor
type ProcessConfig struct {
	Workers     int           // Number of parallel workers
	Timeout     time.Duration // Per-test timeout
	Fork        string        // Filter by fork (empty = all)
	SkipInvalid bool          // Skip invalid tests instead of erroring
	Overwrite   bool          // Overwrite existing files
	DryRun      bool          // Don't write output
	Progress    bool          // Show progress bar
	Quiet       bool          // Suppress non-error output
	Verbose     bool          // Verbose output
}

// DefaultProcessConfig returns sensible defaults
func DefaultProcessConfig() ProcessConfig {
	return ProcessConfig{
		Workers:     runtime.NumCPU(),
		Timeout:     30 * time.Second,
		SkipInvalid: false,
		Overwrite:   false,
		DryRun:      false,
		Progress:    false,
		Quiet:       false,
		Verbose:     false,
	}
}

// processJob represents a single processing task
type processJob struct {
	inputPath string
}

// ProcessResult contains the result of processing a single test
type ProcessResult struct {
	InputPath  string        // Path to input file
	OutputPath string        // Path to output file (if written)
	TraceHash  string        // MD5 hash of normalized trace
	StateRoot  string        // Post-execution state root
	TraceLines int           // Number of trace lines
	Fork       string        // Fork name (if detected)
	Duration   time.Duration // Processing time
	Error      error         // Error if processing failed
	Skipped    bool          // True if test was skipped
	SkipReason string        // Reason for skipping
}

// BatchProcessor handles parallel processing of state tests
type BatchProcessor struct {
	config    ProcessConfig
	inputDir  string
	outputDir string

	// Statistics (atomic)
	totalTests int64
	successful int64
	failed     int64
	skipped    int64

	// Fork counts (protected by mutex)
	forkCounts map[string]int
	forkMu     sync.Mutex

	// Channels for worker coordination
	jobs    chan processJob
	results chan ProcessResult
	done    chan struct{}

	// Synchronization
	wg sync.WaitGroup

	// Output
	manifest *Manifest
	errorLog *ErrorLog

	// Start time
	startTime time.Time
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(inputDir, outputDir string, config ProcessConfig) (*BatchProcessor, error) {
	// Validate input directory
	info, err := os.Stat(inputDir)
	if err != nil {
		return nil, fmt.Errorf("input directory: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("input path is not a directory: %s", inputDir)
	}

	// Create output directory structure
	if !config.DryRun {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return nil, fmt.Errorf("create output directory: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(outputDir, "tests"), 0755); err != nil {
			return nil, fmt.Errorf("create tests subdirectory: %w", err)
		}
	}

	// Initialize error log
	errorLogPath := filepath.Join(outputDir, "errors.log")
	errorLog := NewErrorLog(errorLogPath, config.DryRun)

	return &BatchProcessor{
		config:     config,
		inputDir:   inputDir,
		outputDir:  outputDir,
		forkCounts: make(map[string]int),
		jobs:       make(chan processJob, config.Workers*2),
		results:    make(chan ProcessResult, config.Workers*2),
		done:       make(chan struct{}),
		manifest:   NewManifest(),
		errorLog:   errorLog,
	}, nil
}

// Run executes the batch processing
func (bp *BatchProcessor) Run() error {
	bp.startTime = time.Now()

	// Start workers
	for i := 0; i < bp.config.Workers; i++ {
		bp.wg.Add(1)
		go bp.worker()
	}

	// Start result collector
	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go bp.collector(&collectorWg)

	// Start progress reporter if enabled
	if bp.config.Progress && !bp.config.Quiet {
		go bp.progressReporter()
	}

	// Enumerate and queue jobs
	if err := bp.enqueueJobs(); err != nil {
		close(bp.jobs)
		return fmt.Errorf("enumerate files: %w", err)
	}
	close(bp.jobs)

	// Wait for workers
	bp.wg.Wait()
	close(bp.results)

	// Wait for collector
	collectorWg.Wait()
	close(bp.done)

	// Finalize manifest
	bp.manifest.TotalTests = int(atomic.LoadInt64(&bp.totalTests))
	bp.manifest.SuccessfulTests = int(atomic.LoadInt64(&bp.successful))
	bp.manifest.FailedTests = int(atomic.LoadInt64(&bp.failed))
	bp.manifest.SkippedTests = int(atomic.LoadInt64(&bp.skipped))
	bp.manifest.ProcessingTime = time.Since(bp.startTime)
	bp.manifest.Workers = bp.config.Workers
	bp.manifest.GeneratedAt = time.Now().UTC()

	// Copy fork counts
	bp.forkMu.Lock()
	bp.manifest.ForkCounts = make(map[string]int)
	for k, v := range bp.forkCounts {
		bp.manifest.ForkCounts[k] = v
	}
	bp.forkMu.Unlock()

	// Write manifest
	if !bp.config.DryRun {
		if err := bp.manifest.WriteToFile(filepath.Join(bp.outputDir, "manifest.json")); err != nil {
			return fmt.Errorf("write manifest: %w", err)
		}
	}

	// Close error log
	bp.errorLog.Close()

	// Print summary
	if !bp.config.Quiet {
		bp.printSummary()
	}

	return nil
}

// enqueueJobs walks the input directory and queues processing jobs
func (bp *BatchProcessor) enqueueJobs() error {
	return filepath.Walk(bp.inputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}

		atomic.AddInt64(&bp.totalTests, 1)
		bp.jobs <- processJob{inputPath: path}
		return nil
	})
}

// worker processes jobs from the queue
func (bp *BatchProcessor) worker() {
	defer bp.wg.Done()

	for job := range bp.jobs {
		result := bp.processFile(job.inputPath)
		bp.results <- result
	}
}

// processFile processes a single state test file
func (bp *BatchProcessor) processFile(inputPath string) ProcessResult {
	start := time.Now()
	result := ProcessResult{
		InputPath: inputPath,
	}

	// Read file
	data, err := os.ReadFile(inputPath)
	if err != nil {
		result.Error = fmt.Errorf("read file: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	// Validate
	if !isValidStateTestJSON(data) {
		if bp.config.SkipInvalid {
			result.Skipped = true
			result.SkipReason = "invalid state test format"
			result.Duration = time.Since(start)
			return result
		}
		result.Error = fmt.Errorf("invalid state test format")
		result.Duration = time.Since(start)
		return result
	}

	// Detect fork if filtering
	if bp.config.Fork != "" {
		fork := detectFork(data)
		if fork != "" && fork != bp.config.Fork {
			result.Skipped = true
			result.SkipReason = fmt.Sprintf("fork mismatch: %s (want %s)", fork, bp.config.Fork)
			result.Duration = time.Since(start)
			return result
		}
		result.Fork = fork
	}

	// Execute with tracing
	ctx, cancel := context.WithTimeout(context.Background(), bp.config.Timeout)
	defer cancel()

	traceResult, err := executeWithTracing(ctx, data, bp.config.Timeout)
	if err != nil {
		result.Error = fmt.Errorf("execute: %w", err)
		result.Duration = time.Since(start)
		return result
	}

	result.TraceHash = traceResult.TraceHash
	result.StateRoot = traceResult.StateRoot
	result.TraceLines = traceResult.TraceLines
	result.Duration = time.Since(start)

	// Inject metadata and write output
	if !bp.config.DryRun {
		outputPath, err := bp.writeOutput(inputPath, data, traceResult)
		if err != nil {
			result.Error = fmt.Errorf("write output: %w", err)
			return result
		}
		result.OutputPath = outputPath
	}

	return result
}

// writeOutput writes the enhanced test file
func (bp *BatchProcessor) writeOutput(inputPath string, data []byte, traceResult *TracingResult) (string, error) {
	// Build metadata
	meta := &CrossVMMetadata{
		Comment:        CrossVMDefaultComment,
		GeneratedBy:    "geth",
		TraceHash:      traceResult.TraceHash,
		StateRoot:      traceResult.StateRoot,
		CrossVMVersion: CrossVMMetadataVersion,
		TraceLines:     traceResult.TraceLines,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
		Version:        getGethVersion(),
	}

	// Inject metadata
	enhanced, err := InjectCrossVMMetadata(data, meta)
	if err != nil {
		return "", fmt.Errorf("inject metadata: %w", err)
	}

	// Generate output filename using trace hash prefix.
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
		outputPath := filepath.Join(bp.outputDir, "tests", filename)

		// O_EXCL ensures atomic create - fails if file exists
		f, err := os.OpenFile(outputPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil {
			if os.IsExist(err) {
				continue // File exists, try next counter
			}
			return "", err // Other error
		}
		// Successfully created file exclusively
		_, writeErr := f.Write(enhanced)
		closeErr := f.Close()
		if writeErr != nil {
			return "", writeErr
		}
		if closeErr != nil {
			return "", closeErr
		}
		return outputPath, nil
	}

	return "", fmt.Errorf("failed to find unique filename after 10000 attempts for hash %s", baseFilename)
}

// collector collects results and updates statistics
func (bp *BatchProcessor) collector(wg *sync.WaitGroup) {
	defer wg.Done()

	for result := range bp.results {
		if result.Skipped {
			atomic.AddInt64(&bp.skipped, 1)
			if bp.config.Verbose && !bp.config.Quiet {
				fmt.Printf("SKIP: %s (%s)\n", result.InputPath, result.SkipReason)
			}
		} else if result.Error != nil {
			atomic.AddInt64(&bp.failed, 1)
			bp.errorLog.Log(result.InputPath, result.Error)
			if bp.config.Verbose && !bp.config.Quiet {
				fmt.Printf("FAIL: %s (%v)\n", result.InputPath, result.Error)
			}
		} else {
			atomic.AddInt64(&bp.successful, 1)

			// Track fork counts
			if result.Fork != "" {
				bp.forkMu.Lock()
				bp.forkCounts[result.Fork]++
				bp.forkMu.Unlock()
			}

			if bp.config.Verbose && !bp.config.Quiet {
				fmt.Printf("OK:   %s -> %s (hash=%s)\n",
					result.InputPath, result.OutputPath, result.TraceHash[:16])
			}
		}
	}
}

// progressReporter displays progress periodically
func (bp *BatchProcessor) progressReporter() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			total := atomic.LoadInt64(&bp.totalTests)
			done := atomic.LoadInt64(&bp.successful) +
				atomic.LoadInt64(&bp.failed) +
				atomic.LoadInt64(&bp.skipped)
			var pct float64
			if total > 0 {
				pct = float64(done) / float64(total) * 100
			}
			fmt.Printf("\rProgress: %d/%d (%.1f%%) - OK: %d, FAIL: %d, SKIP: %d   ",
				done, total, pct,
				atomic.LoadInt64(&bp.successful),
				atomic.LoadInt64(&bp.failed),
				atomic.LoadInt64(&bp.skipped))
		case <-bp.done:
			fmt.Println() // Clear progress line
			return
		}
	}
}

// printSummary prints the final summary
func (bp *BatchProcessor) printSummary() {
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println("Processing Complete")
	fmt.Println("========================================")
	fmt.Printf("Total:      %d\n", atomic.LoadInt64(&bp.totalTests))
	fmt.Printf("Successful: %d\n", atomic.LoadInt64(&bp.successful))
	fmt.Printf("Failed:     %d\n", atomic.LoadInt64(&bp.failed))
	fmt.Printf("Skipped:    %d\n", atomic.LoadInt64(&bp.skipped))
	fmt.Printf("Workers:    %d\n", bp.config.Workers)
	fmt.Printf("Duration:   %s\n", time.Since(bp.startTime).Round(time.Millisecond))
	if !bp.config.DryRun {
		fmt.Printf("Output:     %s\n", bp.outputDir)
	}
	fmt.Println("========================================")
}

// InjectCrossVMMetadata injects cross-VM metadata into a test JSON.
// The metadata is added to the "_info" field inside each test object,
// following the EEST (Ethereum Execution Spec Tests) standard format.
func InjectCrossVMMetadata(testJSON []byte, meta *CrossVMMetadata) ([]byte, error) {
	var original map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &original); err != nil {
		return nil, fmt.Errorf("parse test JSON: %w", err)
	}

	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	// Inject _info into each test object
	for testName, testRaw := range original {
		// Skip any metadata keys at top level
		if testName == "_crossvm" || testName == "_info" {
			continue
		}

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
			return nil, fmt.Errorf("re-marshal test object %s: %w", testName, err)
		}
		original[testName] = updatedTestRaw
	}

	// Remove legacy _crossvm key if present
	delete(original, "_crossvm")

	return json.MarshalIndent(original, "", "  ")
}

// detectFork attempts to detect the fork from a state test JSON
func detectFork(testJSON []byte) string {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &raw); err != nil {
		return ""
	}

	for testName, testRaw := range raw {
		if testName == "_crossvm" || testName == "_info" {
			continue
		}

		var testObj struct {
			Post map[string]json.RawMessage `json:"post"`
		}
		if err := json.Unmarshal(testRaw, &testObj); err != nil {
			continue
		}

		// Return the first fork found
		for fork := range testObj.Post {
			return fork
		}
	}

	return ""
}

// ProcessFile is a convenience function to process a single file
func ProcessFile(inputPath string, timeout time.Duration) (*ProcessResult, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, fmt.Errorf("read file: %w", err)
	}

	if !isValidStateTestJSON(data) {
		return nil, fmt.Errorf("invalid state test format")
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	traceResult, err := executeWithTracing(ctx, data, timeout)
	if err != nil {
		return nil, fmt.Errorf("execute: %w", err)
	}

	return &ProcessResult{
		InputPath:  inputPath,
		TraceHash:  traceResult.TraceHash,
		StateRoot:  traceResult.StateRoot,
		TraceLines: traceResult.TraceLines,
	}, nil
}
