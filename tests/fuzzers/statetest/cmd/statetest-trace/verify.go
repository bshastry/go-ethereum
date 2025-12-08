// Copyright 2024 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest"
	"github.com/urfave/cli/v2"
)

var verifyCommand = &cli.Command{
	Name:      "verify",
	Usage:     "Verify trace hashes in an existing enhanced corpus",
	ArgsUsage: "<corpus-dir>",
	Action:    verifyAction,
	Flags:     verifyFlags,
	Description: `
The verify command re-executes tests in an enhanced corpus and compares
the computed trace hashes against the stored values to detect divergences.

This is useful for:
  - Validating that a corpus was generated correctly
  - Detecting changes in EVM behavior after geth updates
  - Cross-checking corpus integrity

Example usage:
  statetest-trace verify ./enhanced_corpus
  statetest-trace verify -v ./enhanced_corpus
  statetest-trace verify -w 32 ./enhanced_corpus
`,
}

type verifyResult struct {
	path           string
	storedHash     string
	computedHash   string
	matched        bool
	noMetadata     bool
	err            error
}

func verifyAction(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("usage: statetest-trace verify <corpus-dir>")
	}

	corpusDir := ctx.Args().Get(0)
	timeout := ctx.Duration(TimeoutFlag.Name)
	verbose := ctx.Bool(VerboseFlag.Name)
	quiet := ctx.Bool(QuietFlag.Name)

	workers := ctx.Int(WorkersFlag.Name)
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	// Load corpus entries
	entries, err := statetest.LoadEnhancedCorpus(corpusDir)
	if err != nil {
		return fmt.Errorf("load corpus: %w", err)
	}

	if len(entries) == 0 {
		return fmt.Errorf("no test files found in %s", corpusDir)
	}

	if !quiet {
		fmt.Printf("Verifying %d test files with %d workers...\n", len(entries), workers)
	}

	// Create job and result channels
	jobs := make(chan statetest.EnhancedCorpusEntry, workers*2)
	results := make(chan verifyResult, workers*2)

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for entry := range jobs {
				result := verifyEntry(entry, timeout)
				results <- result
			}
		}()
	}

	// Start result collector
	var (
		total       int64
		matched     int64
		diverged    int64
		noMetadata  int64
		errors      int64
		divergences []verifyResult
	)

	var collectorWg sync.WaitGroup
	collectorWg.Add(1)
	go func() {
		defer collectorWg.Done()
		for result := range results {
			atomic.AddInt64(&total, 1)

			if result.err != nil {
				atomic.AddInt64(&errors, 1)
				if verbose && !quiet {
					fmt.Printf("ERROR: %s: %v\n", result.path, result.err)
				}
			} else if result.noMetadata {
				atomic.AddInt64(&noMetadata, 1)
				if verbose && !quiet {
					fmt.Printf("SKIP:  %s (no metadata)\n", result.path)
				}
			} else if result.matched {
				atomic.AddInt64(&matched, 1)
				if verbose && !quiet {
					fmt.Printf("OK:    %s\n", result.path)
				}
			} else {
				atomic.AddInt64(&diverged, 1)
				divergences = append(divergences, result)
				if !quiet {
					fmt.Printf("DIVERGED: %s\n", result.path)
					fmt.Printf("  stored:   %s\n", result.storedHash)
					fmt.Printf("  computed: %s\n", result.computedHash)
				}
			}
		}
	}()

	// Enqueue jobs
	for _, entry := range entries {
		jobs <- entry
	}
	close(jobs)

	// Wait for workers
	wg.Wait()
	close(results)

	// Wait for collector
	collectorWg.Wait()

	// Print summary
	if !quiet {
		fmt.Println()
		fmt.Println("========================================")
		fmt.Println("Verification Complete")
		fmt.Println("========================================")
		fmt.Printf("Total:      %d\n", total)
		fmt.Printf("Matched:    %d\n", matched)
		fmt.Printf("Diverged:   %d\n", diverged)
		fmt.Printf("No Meta:    %d\n", noMetadata)
		fmt.Printf("Errors:     %d\n", errors)
		fmt.Println("========================================")

		if diverged > 0 {
			fmt.Println("\nDiverged files:")
			for _, d := range divergences {
				fmt.Printf("  %s\n", d.path)
			}
		}
	}

	// Return error if any divergences found
	if diverged > 0 {
		return fmt.Errorf("%d tests diverged", diverged)
	}

	return nil
}

func verifyEntry(entry statetest.EnhancedCorpusEntry, timeout time.Duration) verifyResult {
	result := verifyResult{
		path: entry.Path,
	}

	// Check if entry has metadata
	if !entry.HasMetadata() {
		result.noMetadata = true
		return result
	}

	result.storedHash = entry.Metadata.TraceHash

	// Strip metadata and re-execute
	testJSON, err := statetest.StripCrossVMMetadata(entry.TestRaw)
	if err != nil {
		result.err = fmt.Errorf("strip metadata: %w", err)
		return result
	}

	// Execute with tracing
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	traceResult, err := statetest.ExecuteAndNormalize(testJSON, timeout)
	if err != nil {
		// Check for context cancellation
		if ctx.Err() != nil {
			result.err = fmt.Errorf("timeout after %s", timeout)
		} else {
			result.err = fmt.Errorf("execute: %w", err)
		}
		return result
	}

	result.computedHash = traceResult.TraceHash
	result.matched = (result.storedHash == result.computedHash)

	return result
}

// VerifyCorpus is the programmatic API for verification
func VerifyCorpus(corpusDir string, timeout time.Duration, workers int) (matched, diverged, errors int, divergedPaths []string, err error) {
	entries, err := statetest.LoadEnhancedCorpus(corpusDir)
	if err != nil {
		return 0, 0, 0, nil, fmt.Errorf("load corpus: %w", err)
	}

	for _, entry := range entries {
		if !entry.HasMetadata() {
			continue
		}

		// Read the original file to get fresh data
		data, readErr := os.ReadFile(entry.Path)
		if readErr != nil {
			errors++
			continue
		}

		// Strip metadata
		testJSON, stripErr := statetest.StripCrossVMMetadata(data)
		if stripErr != nil {
			errors++
			continue
		}

		// Execute
		traceResult, execErr := statetest.ExecuteAndNormalize(testJSON, timeout)
		if execErr != nil {
			errors++
			continue
		}

		// Compare
		if entry.Metadata.TraceHash == traceResult.TraceHash {
			matched++
		} else {
			diverged++
			divergedPaths = append(divergedPaths, filepath.Base(entry.Path))
		}
	}

	return matched, diverged, errors, divergedPaths, nil
}
