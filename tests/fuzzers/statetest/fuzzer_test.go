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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest/mutations"
)

// FuzzStats tracks fuzzer statistics
type FuzzStats struct {
	totalExecs      int64
	totalCrashes    int64
	totalTimeouts   int64
	coverageFinds   int64
	corpusSaved     int64
	crossVMVerified int64 // Cross-VM entries verified
	crossVMFailed   int64 // Cross-VM entries that failed verification (consensus divergence!)

	startTime    time.Time
	numWorkers   int
	strategy     string
	crashDir     string
	crashLogFile string
	crashMu      sync.Mutex
	done         chan struct{}

	// For tracking last find time atomically
	lastFindTime atomic.Value // time.Time
}

// logCrash logs a crash to disk
func (s *FuzzStats) logCrash(input []byte, panicVal interface{}) {
	atomic.AddInt64(&s.totalCrashes, 1)

	s.crashMu.Lock()
	defer s.crashMu.Unlock()

	// Save crash input
	crashID := atomic.LoadInt64(&s.totalCrashes)
	crashFile := filepath.Join(s.crashDir, fmt.Sprintf("crash_%d.json", crashID))
	if err := os.WriteFile(crashFile, input, 0644); err != nil {
		return // Silently fail if we can't write
	}

	// Append to crash log
	logEntry := fmt.Sprintf("[%s] Crash #%d: %v\n",
		time.Now().Format(time.RFC3339), crashID, panicVal)

	f, err := os.OpenFile(filepath.Join(s.crashDir, s.crashLogFile),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(logEntry)
}

// logCrossVMDivergence logs a cross-VM consensus divergence
func (s *FuzzStats) logCrossVMDivergence(input []byte, expectedHash, actualHash, sourceVM string) {
	atomic.AddInt64(&s.crossVMFailed, 1)

	s.crashMu.Lock()
	defer s.crashMu.Unlock()

	// Save divergence input
	divergeID := atomic.LoadInt64(&s.crossVMFailed)
	divergeFile := filepath.Join(s.crashDir, fmt.Sprintf("divergence_%d.json", divergeID))
	if err := os.WriteFile(divergeFile, input, 0644); err != nil {
		return
	}

	// Append to divergence log
	logEntry := fmt.Sprintf("[%s] CONSENSUS DIVERGENCE #%d: source=%s expected=%s actual=%s\n",
		time.Now().Format(time.RFC3339), divergeID, sourceVM, expectedHash, actualHash)

	f, err := os.OpenFile(filepath.Join(s.crashDir, "crossvm_divergences.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(logEntry)
}

// TestFuzzStateTestCustomMutator is the main coverage-guided custom mutator fuzzer.
//
// Run with:
//
//	go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/ -timeout=1h
//	FUZZ_DURATION=24h FUZZ_WORKERS=32 go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/ -timeout=25h
//
// Environment variables:
//
//	FUZZ_DURATION:   How long to run (default: 2m)
//	FUZZ_SEED_DIR:   Seed directory (default: testdata/seeds)
//	FUZZ_WORKERS:    Number of workers (default: NumCPU)
//	FUZZ_STRATEGY:   Mutation strategy (default: combined)
//	FUZZ_CORPUS_DIR: Output corpus directory for trace-enhanced entries (default: testdata/enhanced_corpus)
//
// For A/B testing with generators, see TestFuzzStateTestAB.
func TestFuzzStateTestCustomMutator(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping custom mutator fuzz test in short mode")
	}

	// Configuration from environment
	seedDir := getEnvOrDefault("FUZZ_SEED_DIR", filepath.Join("testdata", "seeds"))
	strategy := getEnvOrDefault("FUZZ_STRATEGY", "combined")
	corpusDir := getEnvOrDefault("FUZZ_CORPUS_DIR", filepath.Join("testdata", "enhanced_corpus"))
	duration := parseDurationOrDefault("FUZZ_DURATION", 2*time.Minute)
	numWorkers := parseIntOrDefault("FUZZ_WORKERS", runtime.NumCPU())
	testTimeout := 5 * time.Second

	// Load seeds
	seeds, err := LoadSeeds(seedDir)
	if err != nil {
		t.Logf("Warning: failed to load seeds from %s: %v", seedDir, err)
	}

	// Use embedded seeds if no external seeds found
	if len(seeds) == 0 {
		t.Logf("No seeds found in %s, using embedded seeds", seedDir)
		seeds = EmbeddedSeeds()
	}

	// Initialize corpus saver for trace-enhanced entries
	corpusSaver, err := NewCorpusSaver(corpusDir)
	if err != nil {
		t.Fatalf("Failed to create corpus saver: %v", err)
	}

	t.Logf("Configuration:")
	t.Logf("  Seeds:      %d", len(seeds))
	t.Logf("  Strategy:   %s", strategy)
	t.Logf("  Duration:   %v", duration)
	t.Logf("  Workers:    %d", numWorkers)
	t.Logf("  Timeout:    %v per test", testTimeout)
	t.Logf("  Corpus Dir: %s", corpusDir)

	// Initialize corpus with coverage guidance
	corpus := NewCoverageCorpus(seeds,
		WithMaxQueueSize(10000),
		WithHighPriorityProbability(0.8),
		WithMaxSplicingPoolSize(10000),
	)

	// Initialize stats
	crashDir := filepath.Join("testdata", "crashes")
	if err := os.MkdirAll(crashDir, 0755); err != nil {
		t.Fatalf("Failed to create crash directory: %v", err)
	}

	stats := &FuzzStats{
		startTime:    time.Now(),
		numWorkers:   numWorkers,
		strategy:     strategy,
		crashDir:     crashDir,
		crashLogFile: "fuzz_crashes.log",
		done:         make(chan struct{}),
	}
	stats.lastFindTime.Store(time.Now())

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		// Each worker gets its own mutator (rand.Rand is not thread-safe)
		workerMutator := mutations.NewRawMutatorWithCorpus(strategy, corpus)
		go worker(i, corpus, workerMutator, &wg, stats, testTimeout, corpusSaver)
	}

	// Progress reporter
	go progressReporter(t, stats, corpus)

	// Run until duration expires
	time.Sleep(duration)
	close(stats.done)
	wg.Wait()

	// Final report
	printFinalReport(t, stats, corpus)
}

// worker runs the fuzzing loop
func worker(
	id int,
	corpus *CoverageCorpus,
	mutator *mutations.RawMutator,
	wg *sync.WaitGroup,
	stats *FuzzStats,
	testTimeout time.Duration,
	corpusSaver *CorpusSaver,
) {
	defer wg.Done()

	for {
		select {
		case <-stats.done:
			return
		default:
		}

		// Get next input (high priority first)
		input := corpus.Pop()
		if input == nil {
			time.Sleep(time.Millisecond)
			continue
		}

		// Check if this is a cross-VM entry that needs verification
		// Use case-insensitive comparison for compatibility with other client implementations
		crossVMMeta := extractCrossVMMetadata(input)
		if crossVMMeta != nil && crossVMMeta.GeneratedBy != "" &&
			!strings.EqualFold(crossVMMeta.GeneratedBy, "geth") {
			// Cross-VM entry from another client - verify instead of mutate
			verifyCrossVMEntry(input, crossVMMeta, stats, testTimeout)
			continue
		}

		// Strip stale metadata before mutation to avoid stale hashes
		cleaned, err := StripCrossVMMetadata(input)
		if err != nil {
			cleaned = input
		}

		// Mutate clean input
		mutated, strategyName, err := mutator.MutateRawJSON(cleaned)
		if err != nil {
			mutated = cleaned
			strategyName = "original"
		}

		// Execute with coverage tracking
		completed, crashed, panicVal, coverageDelta := executeWithCoverageTracking(
			mutated, testTimeout)

		atomic.AddInt64(&stats.totalExecs, 1)

		if !completed {
			atomic.AddInt64(&stats.totalTimeouts, 1)
			continue
		}

		if crashed {
			stats.logCrash(mutated, panicVal)
			continue
		}

		// Coverage-guided prioritization
		if coverageDelta > 0 {
			atomic.AddInt64(&stats.coverageFinds, 1)
			stats.lastFindTime.Store(time.Now())

			// Get trace hash for cross-client comparison
			tracingResult, tracingErr := ExecuteAndNormalize(mutated, testTimeout)
			if tracingErr == nil && tracingResult != nil {
				// Save enhanced corpus entry with trace metadata
				_, saveErr := corpusSaver.SaveEnhancedCorpusEntry(
					mutated,
					tracingResult,
					coverageDelta,
					strategyName,
				)
				if saveErr == nil {
					atomic.AddInt64(&stats.corpusSaved, 1)
				}
			}

			// Add to high priority queue for further mutation
			corpus.AddHighPriority(&PriorityInput{
				Data:           mutated,
				Priority:       int(coverageDelta * 1000000), // Scale for int comparison
				CoverageDelta:  coverageDelta,
				DiscoveredAt:   time.Now(),
				ParentStrategy: strategyName,
			})
		}
	}
}

// extractCrossVMMetadata extracts cross-VM metadata from a test JSON if present.
// It supports both the EEST standard format (_info inside test objects) and
// the legacy format (top-level _crossvm) for backward compatibility.
func extractCrossVMMetadata(testJSON []byte) *CrossVMMetadata {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(testJSON, &raw); err != nil {
		return nil
	}

	// Use the shared helper function from corpus_saver.go
	return extractCrossVMMetadataFromRaw(raw)
}

// verifyCrossVMEntry verifies a cross-VM corpus entry and logs divergences
func verifyCrossVMEntry(input []byte, meta *CrossVMMetadata, stats *FuzzStats, timeout time.Duration) {
	// Execute with trace normalization
	result, err := ExecuteAndNormalize(input, timeout)
	if err != nil {
		// Execution failed - can't verify
		return
	}

	atomic.AddInt64(&stats.crossVMVerified, 1)

	// Compare trace hashes
	if result.TraceHash != meta.TraceHash {
		// CONSENSUS DIVERGENCE DETECTED!
		stats.logCrossVMDivergence(input, meta.TraceHash, result.TraceHash, meta.GeneratedBy)
	}
}

// executeWithCoverageTracking runs a test and returns coverage delta
func executeWithCoverageTracking(
	input []byte,
	timeout time.Duration,
) (completed bool, crashed bool, panicVal interface{}, coverageDelta float64) {
	beforeCov := testing.Coverage()
	completed, crashed, panicVal = executeStateTestWithTimeout(input, timeout)
	afterCov := testing.Coverage()
	coverageDelta = afterCov - beforeCov
	return
}

// executeStateTestWithTimeout executes a state test with timeout protection
func executeStateTestWithTimeout(testJSON []byte, timeout time.Duration) (bool, bool, interface{}) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	crashed, panicVal := executeStateTestWithContext(ctx, testJSON)

	if ctx.Err() == context.DeadlineExceeded {
		return false, false, nil // Timeout
	}
	return true, crashed, panicVal
}

// executeStateTestWithContext executes a state test with cancellation support
func executeStateTestWithContext(ctx context.Context, testJSON []byte) (crashed bool, panicVal interface{}) {
	defer func() {
		if r := recover(); r != nil {
			if isCancellationError(r) {
				// Context cancellation, not a real crash
				crashed = false
				panicVal = nil
				return
			}
			// Real crash
			crashed = true
			panicVal = r
		}
	}()

	var stateTests map[string]tests.StateTest
	if err := json.Unmarshal(testJSON, &stateTests); err != nil {
		return false, nil
	}

	tracer := newCancellationTracer(ctx)

	for _, test := range stateTests {
		for _, subtest := range test.Subtests() {
			if !isSupportedFork(subtest.Fork) {
				continue
			}
			if ctx.Err() != nil {
				return false, nil
			}

			st, _, _, _ := test.RunNoVerify(
				subtest,
				vm.Config{Tracer: tracer.Hooks()},
				false,
				rawdb.HashScheme,
			)
			if st.StateDB != nil {
				st.Close()
			}
		}
	}
	return false, nil
}

// progressReporter logs progress periodically
func progressReporter(t *testing.T, stats *FuzzStats, corpus *CoverageCorpus) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastCoverage float64
	var lastCoverageTime time.Time

	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(stats.startTime)
			execs := atomic.LoadInt64(&stats.totalExecs)
			crashes := atomic.LoadInt64(&stats.totalCrashes)
			timeouts := atomic.LoadInt64(&stats.totalTimeouts)
			covFinds := atomic.LoadInt64(&stats.coverageFinds)
			rate := float64(execs) / elapsed.Seconds()

			// Get current coverage
			currentCov := testing.Coverage() * 100 // Convert to percentage

			// Calculate coverage growth rate
			var growthRate float64
			if !lastCoverageTime.IsZero() {
				timeDelta := time.Since(lastCoverageTime).Minutes()
				if timeDelta > 0 {
					growthRate = (currentCov - lastCoverage) / timeDelta
				}
			}
			lastCoverage = currentCov
			lastCoverageTime = time.Now()

			// Time since last coverage find
			sinceLastFind := "never"
			if lastFind, ok := stats.lastFindTime.Load().(time.Time); ok {
				sinceLastFind = time.Since(lastFind).Round(time.Second).String()
			}

			// Corpus stats
			hpQueueLen, splicingLen, _, _ := corpus.Stats()
			seedCount := corpus.SeedCount()

			corpusSaved := atomic.LoadInt64(&stats.corpusSaved)
			crossVMVerified := atomic.LoadInt64(&stats.crossVMVerified)
			crossVMFailed := atomic.LoadInt64(&stats.crossVMFailed)

			t.Logf("")
			t.Logf("═══════════════════════════════════════════════════════════════════")
			t.Logf(" RUNTIME: %-12s  WORKERS: %-4d  STRATEGY: %s",
				elapsed.Round(time.Second), stats.numWorkers, stats.strategy)
			t.Logf("───────────────────────────────────────────────────────────────────")
			t.Logf(" EXEC: %d (%.0f/s)  CRASH: %d  TIMEOUT: %d",
				execs, rate, crashes, timeouts)
			t.Logf(" COV:  %.3f%%  FINDS: %d  LAST: %s  GROWTH: %+.4f%%/min",
				currentCov, covFinds, sinceLastFind, growthRate)
			t.Logf(" SEEDS: %d  HP_QUEUE: %d  SPLICING: %d  SAVED: %d",
				seedCount, hpQueueLen, splicingLen, corpusSaved)
			if crossVMVerified > 0 || crossVMFailed > 0 {
				t.Logf(" CROSS-VM: verified=%d  divergences=%d", crossVMVerified, crossVMFailed)
			}
			t.Logf("═══════════════════════════════════════════════════════════════════")

		case <-stats.done:
			return
		}
	}
}

// printFinalReport prints the final fuzzing report
func printFinalReport(t *testing.T, stats *FuzzStats, corpus *CoverageCorpus) {
	elapsed := time.Since(stats.startTime)
	execs := atomic.LoadInt64(&stats.totalExecs)
	crashes := atomic.LoadInt64(&stats.totalCrashes)
	timeouts := atomic.LoadInt64(&stats.totalTimeouts)
	covFinds := atomic.LoadInt64(&stats.coverageFinds)
	corpusSaved := atomic.LoadInt64(&stats.corpusSaved)
	crossVMVerified := atomic.LoadInt64(&stats.crossVMVerified)
	crossVMFailed := atomic.LoadInt64(&stats.crossVMFailed)
	rate := float64(execs) / elapsed.Seconds()
	currentCov := testing.Coverage() * 100

	hpQueueLen, splicingLen, _, _ := corpus.Stats()
	seedCount := corpus.SeedCount()

	t.Logf("")
	t.Logf("╔═══════════════════════════════════════════════════════════════════╗")
	t.Logf("║                        FINAL RESULTS                              ║")
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Duration:        %-48s ║", elapsed.Round(time.Second))
	t.Logf("║ Total execs:     %-48d ║", execs)
	t.Logf("║ Exec rate:       %-48s ║", fmt.Sprintf("%.0f/sec", rate))
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Final coverage:  %-48s ║", fmt.Sprintf("%.3f%%", currentCov))
	t.Logf("║ Coverage finds:  %-48d ║", covFinds)
	t.Logf("║ Seeds:           %-48d ║", seedCount)
	t.Logf("║ HP queue:        %-48d ║", hpQueueLen)
	t.Logf("║ Splicing pool:   %-48d ║", splicingLen)
	t.Logf("║ Corpus saved:    %-48d ║", corpusSaved)
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Crashes found:   %-48d ║", crashes)
	t.Logf("║ Timeouts:        %-48d ║", timeouts)
	if crossVMVerified > 0 || crossVMFailed > 0 {
		t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
		t.Logf("║ Cross-VM verified: %-46d ║", crossVMVerified)
		t.Logf("║ Cross-VM diverged: %-46d ║", crossVMFailed)
	}
	t.Logf("╚═══════════════════════════════════════════════════════════════════╝")

	if crashes > 0 {
		t.Logf("")
		t.Logf("Crash files saved to: %s", stats.crashDir)
		t.Logf("Crash log: %s", filepath.Join(stats.crashDir, stats.crashLogFile))
	}

	if crossVMFailed > 0 {
		t.Logf("")
		t.Logf("CONSENSUS DIVERGENCES DETECTED!")
		t.Logf("Divergence files saved to: %s", stats.crashDir)
		t.Logf("Divergence log: %s", filepath.Join(stats.crashDir, "crossvm_divergences.log"))
	}

	// Coverage guidance effectiveness
	if covFinds > 0 {
		t.Logf("")
		t.Logf("Coverage guidance stats:")
		t.Logf("   - Avg execs per find: %.0f", float64(execs)/float64(covFinds))
		if execs > 1000000 {
			t.Logf("   - Coverage per 1M execs: %.3f%%", currentCov/(float64(execs)/1000000))
		}
	}
}

// Helper functions for environment configuration (wrappers for local use)

func getEnvOrDefault(key, defaultVal string) string {
	return GetEnvOrDefault(key, defaultVal)
}

func parseDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	return ParseDurationOrDefault(key, defaultVal)
}

func parseIntOrDefault(key string, defaultVal int) int {
	return ParseIntOrDefault(key, defaultVal)
}

// TestNoGoroutineLeakOnTimeout verifies that timeouts don't cause goroutine leaks
func TestNoGoroutineLeakOnTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping goroutine leak test in short mode")
	}

	// Create a test that will loop forever
	infiniteLoopTest := `{
  "infiniteLoopTest": {
    "env": {
      "currentCoinbase": "0x2adc25665018aa1fe0e6bc666dac8fc2697ff9ba",
      "currentDifficulty": "0x20000",
      "currentGasLimit": "0xffffffffffffffff",
      "currentNumber": "0x01",
      "currentTimestamp": "0x03e8",
      "previousHash": "0x0000000000000000000000000000000000000000000000000000000000000000",
      "currentBaseFee": "0x0a"
    },
    "pre": {
      "0xa94f5374fce5edbc8e2a8697c15331677e6ebf0b": {
        "balance": "0xffffffffffffffff",
        "code": "0x",
        "nonce": "0x00",
        "storage": {}
      },
      "0xcccccccccccccccccccccccccccccccccccccccc": {
        "balance": "0x00",
        "code": "0x5b600056",
        "nonce": "0x00",
        "storage": {}
      }
    },
    "transaction": {
      "data": ["0x"],
      "gasLimit": ["0xffffffffffffffff"],
      "gasPrice": "0x0a",
      "nonce": "0x00",
      "secretKey": "0x45a915e4d060149eb4365960e6a7a45f334393093061116b197e3240065ff2d8",
      "to": "0xcccccccccccccccccccccccccccccccccccccccc",
      "value": ["0x00"]
    },
    "post": {
      "London": [
        {
          "hash": "0x0000000000000000000000000000000000000000000000000000000000000000",
          "indexes": {"data": 0, "gas": 0, "value": 0}
        }
      ]
    }
  }
}`

	initialGoroutines := runtime.NumGoroutine()
	timeout := 100 * time.Millisecond

	// Run the test several times
	for i := 0; i < 5; i++ {
		completed, _, _ := executeStateTestWithTimeout([]byte(infiniteLoopTest), timeout)
		if completed {
			t.Error("Expected timeout, but test completed")
		}
	}

	// Allow goroutines to clean up
	time.Sleep(200 * time.Millisecond)
	runtime.GC()

	finalGoroutines := runtime.NumGoroutine()
	leakedGoroutines := finalGoroutines - initialGoroutines

	// Allow for some variance (e.g., runtime goroutines)
	if leakedGoroutines > 2 {
		t.Errorf("Goroutine leak detected: started with %d, ended with %d (leaked %d)",
			initialGoroutines, finalGoroutines, leakedGoroutines)
	}
}

// BenchmarkCustomMutatorFuzzer benchmarks the fuzzer throughput
func BenchmarkCustomMutatorFuzzer(b *testing.B) {
	seeds := EmbeddedSeeds()
	corpus := NewCoverageCorpus(seeds)
	mutator := mutations.NewRawMutatorWithCorpus("combined", corpus)
	timeout := 5 * time.Second

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		input := corpus.Pop()
		if input == nil {
			continue
		}

		mutated, _, err := mutator.MutateRawJSON(input)
		if err != nil {
			mutated = input
		}

		executeStateTestWithTimeout(mutated, timeout)
	}
}

// TestFuzzStateTestAB is the A/B testing entry point for comparing mutation vs generation.
//
// Run with:
//
//	# Mutation only (baseline)
//	FUZZ_PROVIDER=mutation FUZZ_DURATION=1h go test -cover -run=TestFuzzStateTestAB -v ./tests/fuzzers/statetest/
//
//	# Generation only (requires -tags=generators)
//	FUZZ_PROVIDER=generation FUZZ_FORK=Prague FUZZ_DURATION=1h go test -cover -tags=generators -run=TestFuzzStateTestAB -v ./tests/fuzzers/statetest/
//
//	# Hybrid (configurable mix)
//	FUZZ_PROVIDER=hybrid FUZZ_MUTATION_RATIO=0.7 FUZZ_FORK=Prague FUZZ_DURATION=1h go test -cover -tags=generators -run=TestFuzzStateTestAB -v ./tests/fuzzers/statetest/
//
// Environment variables:
//
//	FUZZ_PROVIDER:        Provider type: mutation, generation, hybrid (default: mutation)
//	FUZZ_MUTATION_RATIO:  For hybrid: ratio of mutation vs generation 0.0-1.0 (default: 0.7)
//	FUZZ_ADAPTIVE_RATIO:  For hybrid: enable adaptive ratio adjustment (default: false)
//	FUZZ_FORK:            Target fork for generation (default: Prague)
//	FUZZ_GENERATORS:      Comma-separated list of generators to use (default: all)
//	FUZZ_DURATION:        How long to run (default: 2m)
//	FUZZ_SEED_DIR:        Seed directory (default: testdata/seeds)
//	FUZZ_WORKERS:         Number of workers (default: NumCPU)
//	FUZZ_STRATEGY:        Mutation strategy (default: combined)
//	FUZZ_CORPUS_DIR:      Output corpus directory (default: testdata/enhanced_corpus)
func TestFuzzStateTestAB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping A/B fuzz test in short mode")
	}

	// Configuration from environment
	providerType := getEnvOrDefault("FUZZ_PROVIDER", "mutation")
	seedDir := getEnvOrDefault("FUZZ_SEED_DIR", filepath.Join("testdata", "seeds"))
	strategy := getEnvOrDefault("FUZZ_STRATEGY", "combined")
	corpusDir := getEnvOrDefault("FUZZ_CORPUS_DIR", filepath.Join("testdata", "enhanced_corpus"))
	duration := parseDurationOrDefault("FUZZ_DURATION", 2*time.Minute)
	numWorkers := parseIntOrDefault("FUZZ_WORKERS", runtime.NumCPU())
	testTimeout := 5 * time.Second

	// Load seeds
	seeds, err := LoadSeeds(seedDir)
	if err != nil {
		t.Logf("Warning: failed to load seeds from %s: %v", seedDir, err)
	}
	if len(seeds) == 0 {
		t.Logf("No seeds found in %s, using embedded seeds", seedDir)
		seeds = EmbeddedSeeds()
	}

	// Initialize corpus
	corpus := NewCoverageCorpus(seeds,
		WithMaxQueueSize(10000),
		WithHighPriorityProbability(0.8),
		WithMaxSplicingPoolSize(10000),
	)

	// Initialize corpus saver
	corpusSaver, err := NewCorpusSaver(corpusDir)
	if err != nil {
		t.Fatalf("Failed to create corpus saver: %v", err)
	}

	// Create provider based on configuration
	var provider InputProvider
	switch providerType {
	case "mutation":
		mutator := mutations.NewRawMutatorWithCorpus(strategy, corpus)
		provider = NewMutationProvider(corpus, mutator)

	case "generation":
		// Generation provider is created in generator_provider.go with build tag
		provider = createGeneratorProvider(t, corpus)
		if provider == nil {
			t.Skip("generation provider requires -tags=generators build flag")
		}

	case "hybrid":
		// Hybrid provider is created in hybrid_provider.go
		provider = createHybridProvider(t, corpus, strategy)
		if provider == nil {
			t.Skip("hybrid provider requires -tags=generators build flag")
		}

	default:
		t.Fatalf("Unknown provider type: %s (expected: mutation, generation, hybrid)", providerType)
	}

	t.Logf("A/B Test Configuration:")
	t.Logf("  Provider:   %s", provider.Name())
	t.Logf("  Seeds:      %d", len(seeds))
	t.Logf("  Strategy:   %s", strategy)
	t.Logf("  Duration:   %v", duration)
	t.Logf("  Workers:    %d", numWorkers)
	t.Logf("  Timeout:    %v per test", testTimeout)
	t.Logf("  Corpus Dir: %s", corpusDir)

	// Initialize stats
	crashDir := filepath.Join("testdata", "crashes")
	if err := os.MkdirAll(crashDir, 0755); err != nil {
		t.Fatalf("Failed to create crash directory: %v", err)
	}

	stats := &FuzzStats{
		startTime:    time.Now(),
		numWorkers:   numWorkers,
		strategy:     provider.Name(),
		crashDir:     crashDir,
		crashLogFile: "fuzz_crashes.log",
		done:         make(chan struct{}),
	}
	stats.lastFindTime.Store(time.Now())

	// Start workers using InputProvider
	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go workerWithProvider(i, provider, &wg, stats, testTimeout, corpusSaver)
	}

	// Progress reporter with provider stats
	go progressReporterAB(t, stats, provider)

	// Run until duration expires
	time.Sleep(duration)
	close(stats.done)
	wg.Wait()

	// Final report with A/B metrics
	printFinalReportAB(t, stats, provider)
}

// workerWithProvider runs the fuzzing loop using an InputProvider.
// This is the provider-abstracted version of the worker function.
func workerWithProvider(
	id int,
	provider InputProvider,
	wg *sync.WaitGroup,
	stats *FuzzStats,
	testTimeout time.Duration,
	corpusSaver *CorpusSaver,
) {
	defer wg.Done()

	for {
		select {
		case <-stats.done:
			return
		default:
		}

		// Get next input from provider
		input, source, err := provider.Next()
		if err != nil {
			if err == ErrProviderExhausted {
				time.Sleep(time.Millisecond)
				continue
			}
			// Other errors - skip this iteration
			continue
		}

		// Check for cross-VM verification
		if strings.HasPrefix(source, "crossvm:") {
			crossVMMeta := extractCrossVMMetadata(input)
			if crossVMMeta != nil {
				verifyCrossVMEntry(input, crossVMMeta, stats, testTimeout)
			}
			continue
		}

		// Execute with coverage tracking
		completed, crashed, panicVal, coverageDelta := executeWithCoverageTracking(
			input, testTimeout)

		atomic.AddInt64(&stats.totalExecs, 1)

		if !completed {
			atomic.AddInt64(&stats.totalTimeouts, 1)
			continue
		}

		if crashed {
			stats.logCrash(input, panicVal)
			continue
		}

		// Provide feedback to provider (handles corpus update internally)
		provider.Feedback(input, source, coverageDelta)

		// Coverage-guided actions
		if coverageDelta > 0 {
			atomic.AddInt64(&stats.coverageFinds, 1)
			stats.lastFindTime.Store(time.Now())

			// Get trace hash for cross-client comparison
			tracingResult, tracingErr := ExecuteAndNormalize(input, testTimeout)
			if tracingErr == nil && tracingResult != nil {
				// Save enhanced corpus entry with trace metadata
				_, saveErr := corpusSaver.SaveEnhancedCorpusEntry(
					input,
					tracingResult,
					coverageDelta,
					source,
				)
				if saveErr == nil {
					atomic.AddInt64(&stats.corpusSaved, 1)
				}
			}
		}
	}
}

// progressReporterAB logs progress with provider-specific stats.
func progressReporterAB(t *testing.T, stats *FuzzStats, provider InputProvider) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	var lastCoverage float64
	var lastCoverageTime time.Time

	for {
		select {
		case <-ticker.C:
			elapsed := time.Since(stats.startTime)
			execs := atomic.LoadInt64(&stats.totalExecs)
			crashes := atomic.LoadInt64(&stats.totalCrashes)
			timeouts := atomic.LoadInt64(&stats.totalTimeouts)
			covFinds := atomic.LoadInt64(&stats.coverageFinds)
			rate := float64(execs) / elapsed.Seconds()

			// Get current coverage
			currentCov := testing.Coverage() * 100

			// Calculate coverage growth rate
			var growthRate float64
			if !lastCoverageTime.IsZero() {
				timeDelta := time.Since(lastCoverageTime).Minutes()
				if timeDelta > 0 {
					growthRate = (currentCov - lastCoverage) / timeDelta
				}
			}
			lastCoverage = currentCov
			lastCoverageTime = time.Now()

			// Time since last coverage find
			sinceLastFind := "never"
			if lastFind, ok := stats.lastFindTime.Load().(time.Time); ok {
				sinceLastFind = time.Since(lastFind).Round(time.Second).String()
			}

			// Provider stats
			provStats := provider.Stats()
			corpusSaved := atomic.LoadInt64(&stats.corpusSaved)

			t.Logf("")
			t.Logf("═══════════════════════════════════════════════════════════════════")
			t.Logf(" PROVIDER: %-12s  WORKERS: %-4d  RUNTIME: %s",
				provider.Name(), stats.numWorkers, elapsed.Round(time.Second))
			t.Logf("───────────────────────────────────────────────────────────────────")
			t.Logf(" EXEC: %d (%.0f/s)  CRASH: %d  TIMEOUT: %d",
				execs, rate, crashes, timeouts)
			t.Logf(" COV:  %.3f%%  FINDS: %d  LAST: %s  GROWTH: %+.4f%%/min",
				currentCov, covFinds, sinceLastFind, growthRate)
			t.Logf(" PROVIDER_FINDS: %d  SAVED: %d  FIND_RATE: %.4f%%",
				provStats.CoverageFinds, corpusSaved, provStats.FindRate()*100)

			// Show mutation vs generation totals if available (for hybrid provider)
			if mutStats, ok := provStats.SourceBreakdown["[mutation_total]"]; ok {
				if genStats, ok := provStats.SourceBreakdown["[generation_total]"]; ok {
					t.Logf(" MUT: %d/%d (%.2f%%)  GEN: %d/%d (%.2f%%)",
						mutStats.CoverageFinds, mutStats.Inputs, mutStats.FindRate()*100,
						genStats.CoverageFinds, genStats.Inputs, genStats.FindRate()*100)
				}
			}

			// Show top individual sources if available (excludes aggregates)
			topSources := provStats.TopSources(3)
			if len(topSources) > 0 {
				t.Logf(" TOP_SOURCES: %s", strings.Join(topSources, ", "))
			}

			t.Logf("═══════════════════════════════════════════════════════════════════")

		case <-stats.done:
			return
		}
	}
}

// printFinalReportAB prints the final A/B testing report.
func printFinalReportAB(t *testing.T, stats *FuzzStats, provider InputProvider) {
	elapsed := time.Since(stats.startTime)
	execs := atomic.LoadInt64(&stats.totalExecs)
	crashes := atomic.LoadInt64(&stats.totalCrashes)
	timeouts := atomic.LoadInt64(&stats.totalTimeouts)
	covFinds := atomic.LoadInt64(&stats.coverageFinds)
	corpusSaved := atomic.LoadInt64(&stats.corpusSaved)
	crossVMVerified := atomic.LoadInt64(&stats.crossVMVerified)
	crossVMFailed := atomic.LoadInt64(&stats.crossVMFailed)
	rate := float64(execs) / elapsed.Seconds()
	currentCov := testing.Coverage() * 100

	provStats := provider.Stats()

	t.Logf("")
	t.Logf("╔═══════════════════════════════════════════════════════════════════╗")
	t.Logf("║                     A/B TEST FINAL RESULTS                        ║")
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Provider:        %-48s ║", provider.Name())
	t.Logf("║ Duration:        %-48s ║", elapsed.Round(time.Second))
	t.Logf("║ Total execs:     %-48d ║", execs)
	t.Logf("║ Exec rate:       %-48s ║", fmt.Sprintf("%.0f/sec", rate))
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Final coverage:  %-48s ║", fmt.Sprintf("%.3f%%", currentCov))
	t.Logf("║ Coverage finds:  %-48d ║", covFinds)
	t.Logf("║ Find rate:       %-48s ║", fmt.Sprintf("%.4f%%", provStats.FindRate()*100))
	t.Logf("║ Corpus saved:    %-48d ║", corpusSaved)
	t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
	t.Logf("║ Crashes found:   %-48d ║", crashes)
	t.Logf("║ Timeouts:        %-48d ║", timeouts)

	if crossVMVerified > 0 || crossVMFailed > 0 {
		t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
		t.Logf("║ Cross-VM verified: %-46d ║", crossVMVerified)
		t.Logf("║ Cross-VM diverged: %-46d ║", crossVMFailed)
	}

	// Source breakdown
	if len(provStats.SourceBreakdown) > 0 {
		t.Logf("╠═══════════════════════════════════════════════════════════════════╣")
		t.Logf("║                       SOURCE BREAKDOWN                            ║")
		t.Logf("╠═══════════════════════════════════════════════════════════════════╣")

		// Sort all sources by coverage finds (including totals)
		topSources := provStats.TopSourcesWithTotals(15)
		for _, source := range topSources {
			srcStats := provStats.SourceBreakdown[source]
			if srcStats != nil && srcStats.Inputs > 0 {
				t.Logf("║ %-20s inputs=%-8d finds=%-6d rate=%.3f%% ║",
					truncateString(source, 20),
					srcStats.Inputs,
					srcStats.CoverageFinds,
					srcStats.FindRate()*100)
			}
		}
	}

	t.Logf("╚═══════════════════════════════════════════════════════════════════╝")

	if crashes > 0 {
		t.Logf("")
		t.Logf("Crash files saved to: %s", stats.crashDir)
	}

	if crossVMFailed > 0 {
		t.Logf("")
		t.Logf("CONSENSUS DIVERGENCES DETECTED!")
		t.Logf("Divergence files saved to: %s", stats.crashDir)
	}

	// Coverage guidance effectiveness
	if covFinds > 0 {
		t.Logf("")
		t.Logf("Coverage guidance stats:")
		t.Logf("   - Avg execs per find: %.0f", float64(execs)/float64(covFinds))
		t.Logf("   - Avg delta per find: %.6f%%", provStats.AvgDeltaPerFind()*100)
	}
}

// truncateString truncates a string to maxLen, adding "..." if truncated.
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// parseFloatOrDefault parses a float from environment or returns default.
func parseFloatOrDefault(key string, defaultVal float64) float64 {
	return ParseFloatOrDefault(key, defaultVal)
}

// parseBoolOrDefault parses a bool from environment or returns default.
func parseBoolOrDefault(key string, defaultVal bool) bool {
	return ParseBoolOrDefault(key, defaultVal)
}
