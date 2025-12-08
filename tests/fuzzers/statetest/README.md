# Coverage-Guided State Test Fuzzer

A high-performance, coverage-guided fuzzer for Ethereum state tests, ported from [goevmlab](https://github.com/holiman/goevmlab) with enhancements for coverage-guided prioritization and cross-client differential testing.

## Features

- **Coverage-Guided Prioritization**: Uses Go's `testing.Coverage()` API to track code coverage and prioritize inputs that discover new execution paths
- **15 Ethereum-Aware Mutation Strategies**: Including bytecode, arithmetic, boundary values, gas, storage, and AFL-style splicing
- **Worker Pool Architecture**: Parallel execution with configurable worker count
- **Timeout Protection**: Cancellation tracer prevents infinite loops from blocking workers
- **Cross-Client Trace Normalization**: MD5 hash of normalized traces for differential testing against other clients
- **Rich Progress UI**: Real-time statistics showing executions/sec, coverage %, finds, etc.

## Quick Start

### Basic Run (2 minutes)

```bash
go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/
```

### Using goevmlab Corpus

```bash
FUZZ_SEED_DIR=../goevmlab/corpus go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/
```

### Extended Run (24 hours, 32 workers)

```bash
FUZZ_DURATION=24h FUZZ_WORKERS=32 go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/ -timeout=25h
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `FUZZ_DURATION` | `2m` | How long to run the fuzzer |
| `FUZZ_WORKERS` | `NumCPU` | Number of parallel workers |
| `FUZZ_SEED_DIR` | `testdata/seeds` | Directory containing seed JSON files |
| `FUZZ_STRATEGY` | `combined` | Mutation strategy to use |
| `FUZZ_CORPUS_DIR` | `testdata/enhanced_corpus` | Output directory for trace-enhanced corpus entries |

## Mutation Strategies

| Strategy | Description |
|----------|-------------|
| `bytecode` | Opcode-smart bytecode mutations |
| `opcode-smart` | Replace opcodes with semantically related ones |
| `arithmetic` | AFL-style ±1, ±MAX mutations on numeric fields |
| `boundary` | EVM-specific boundary values (0, 1, MAX_UINT256, gas limits) |
| `gas` | Gas limit mutations with interesting values |
| `value` | Transaction value mutations |
| `storage` | Pre-state storage slot mutations |
| `calldata` | Transaction data mutations |
| `txfields` | Transaction field mutations (nonce, gasPrice, to, etc.) |
| `accountfields` | Account balance and nonce mutations |
| `blockops` | AFL-style block operations (delete, clone, insert) |
| `dictionary` | Dictionary-based mutations with EVM tokens |
| `bitflip` | Single bit flip mutations |
| `havoc` | Stacked mutations (2-128 random mutations) |
| `splicing` | AFL-style splicing combining bytecode from corpus |
| `combined` | Weighted random selection from all strategies (default) |

### Using Specific Strategies

```bash
# Single strategy
FUZZ_STRATEGY=bytecode go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/

# The "combined" strategy (default) randomly selects from all available strategies
FUZZ_STRATEGY=combined go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/
```

## Architecture

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                    Coverage-Guided Custom Mutator Fuzzer                      │
├──────────────────────────────────────────────────────────────────────────────┤
│                                                                              │
│  ┌─────────────┐      ┌────────────────────────────────────────────────────┐│
│  │ Seed Corpus │─────▶│           Priority Queue (thread-safe)             ││
│  │   (JSON)    │      │  ┌────────────────────────────────────────────────┐││
│  └─────────────┘      │  │ High Priority: Inputs that found new coverage  │││
│                       │  │ (sorted by coverage delta, most recent first)  │││
│        │              │  ├────────────────────────────────────────────────┤││
│        │              │  │ Normal Priority: Seeds cycling through         │││
│        │              │  │ (round-robin through original corpus)          │││
│        ▼              │  └────────────────────────────────────────────────┘││
│  ┌───────────┐        └────────────────────────────────────────────────────┘│
│  │ Splicing  │◀──────────────────────────┐                                  │
│  │  Corpus   │        (inputs added when │                                  │
│  │ Provider  │         they find new cov)│                                  │
│  └───────────┘                           │                                  │
│        │                                 │                                  │
│        ▼                                 │                                  │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │                       Worker Pool (N workers)                           ││
│  │  ┌─────────────────────────────────────────────────────────────────────┐││
│  │  │ Worker Loop:                                                        │││
│  │  │  1. Pop input from priority queue                                   │││
│  │  │  2. Get current coverage: before := testing.Coverage()              │││
│  │  │  3. Strip stale _fuzzer metadata, then mutate input                 │││
│  │  │  4. Execute with timeout via cancellationTracer                     │││
│  │  │  5. Get new coverage: after := testing.Coverage()                   │││
│  │  │  6. If after > before:                                              │││
│  │  │     - Generate trace hash via ExecuteAndNormalize                   │││
│  │  │     - Save enhanced corpus entry with gethTraceHash                 │││
│  │  │     - Add mutated input to HIGH priority queue                      │││
│  │  │     - Add to splicing corpus                                        │││
│  │  │  7. Record stats (execs, crashes, timeouts, coverage, saved)        │││
│  │  └─────────────────────────────────────────────────────────────────────┘││
│  └─────────────────────────────────────────────────────────────────────────┘│
│                                                                              │
│  ┌─────────────────────────────────────────────────────────────────────────┐│
│  │ Progress Reporter (every 3s)                                            ││
│  │ - execs/sec, crashes, timeouts                                          ││
│  │ - current coverage %, coverage growth rate                              ││
│  │ - priority queue depth, corpus size                                     ││
│  └─────────────────────────────────────────────────────────────────────────┘│
└──────────────────────────────────────────────────────────────────────────────┘
```

## Output Example

```
═══════════════════════════════════════════════════════════════════
 RUNTIME: 30s           WORKERS: 22    STRATEGY: combined
───────────────────────────────────────────────────────────────────
 EXEC: 186005 (6197/s)  CRASH: 0  TIMEOUT: 0
 COV:  31.646%  FINDS: 31  LAST: 27s  GROWTH: +0.0000%/min
 QUEUE: 0  CORPUS: 31  SAVED: 31
═══════════════════════════════════════════════════════════════════

╔═══════════════════════════════════════════════════════════════════╗
║                        FINAL RESULTS                              ║
╠═══════════════════════════════════════════════════════════════════╣
║ Duration:        30s                                              ║
║ Total execs:     186029                                           ║
║ Exec rate:       6195/sec                                         ║
╠═══════════════════════════════════════════════════════════════════╣
║ Final coverage:  31.646%                                          ║
║ Coverage finds:  31                                               ║
║ Corpus size:     31                                               ║
║ Queue depth:     0                                                ║
╠═══════════════════════════════════════════════════════════════════╣
║ Crashes found:   0                                                ║
║ Timeouts:        0                                                ║
║ Corpus saved:    31                                               ║
╚═══════════════════════════════════════════════════════════════════╝

📊 Coverage guidance stats:
   - Avg execs per find: 6001
```

## File Structure

```
tests/fuzzers/statetest/
├── README.md              # This file
├── fuzzer_test.go         # Main fuzzer with worker pool
├── corpus.go              # Coverage-guided priority queue
├── tracer.go              # Cancellation tracer for timeouts
├── seeds.go               # Seed loading utilities
├── normalizer.go          # Cross-client trace normalization
├── tracing.go             # Tracing execution wrapper
├── corpus_saver.go        # Enhanced corpus saving
├── mutations/
│   ├── strategy.go        # Core interfaces
│   ├── integration.go     # RawMutator adapter
│   ├── bytecode.go        # Bytecode mutations
│   ├── arithmetic.go      # AFL-style arithmetic
│   ├── boundary.go        # Boundary values
│   ├── gas.go             # Gas mutations
│   ├── value.go           # Value mutations
│   ├── storage.go         # Storage mutations
│   ├── calldata.go        # Calldata mutations
│   ├── txfields.go        # Transaction fields
│   ├── accountfields.go   # Account fields
│   ├── blockops.go        # Block operations
│   ├── dictionary.go      # Dictionary-based
│   ├── bitflip.go         # Bit flips
│   ├── havoc.go           # Stacked mutations
│   ├── splicing.go        # AFL splicing
│   └── mutations_test.go  # Tests
└── testdata/
    ├── seeds/             # Initial seed corpus
    ├── enhanced_corpus/   # Coverage-finding inputs with gethTraceHash
    └── crashes/           # Crash-inducing inputs
```

## Cross-Client Differential Testing

The fuzzer includes trace normalization for cross-client comparison:

1. **Trace Normalization**: Converts EVM traces to a canonical format
2. **Trace Hashing**: MD5 hash of normalized traces for comparison
3. **Enhanced Corpus**: Coverage-finding inputs saved with trace hashes

This enables replaying the corpus against other Ethereum clients (Nethermind, Besu, Erigon) to detect consensus divergences.

### Trace Hash Format

When inputs discover new coverage, they are saved to `testdata/enhanced_corpus/` with embedded metadata:

```json
{
  "testName": {
    "env": {...},
    "pre": {...},
    "transaction": {...},
    "post": {...}
  },
  "_fuzzer": {
    "gethTraceHash": "13251158d97ea2420d51501e32f818fc",
    "stateRoot": "0x60a3fe53c5486f7967c947766fce4e08a7ebd8f3...",
    "traceLines": 7,
    "gasUsed": 118,
    "gethVersion": "dev",
    "generatedAt": "2025-12-08T10:50:16Z",
    "coverageDelta": 0.00571,
    "mutationStrategy": "gas"
  }
}
```

### Cross-VM Verification

Other Ethereum clients can verify trace consistency by:

1. Reading the `_fuzzer.gethTraceHash` from corpus entries
2. Executing the test with their own normalized tracing
3. Comparing their trace hash with the stored `gethTraceHash`
4. Reporting divergences if hashes differ

The `gethTraceHash` uses MD5 hashing of normalized trace output (opcode, depth, gas, stack top 6 values) for deterministic cross-client comparison.

## Performance

Typical performance on a modern machine:
- **6,000-10,000 executions/second** with 22 workers
- **30%+ coverage** achieved within first minute
- Coverage-guided prioritization adds 30+ unique inputs to corpus quickly

## Crash Handling

Crashes are automatically saved to `testdata/crashes/`:
- `crash_N.json`: The input that caused the crash
- `fuzz_crashes.log`: Log of all crashes with timestamps

## Credits

Based on the custom mutator fuzzer from [goevmlab](https://github.com/holiman/goevmlab) by Martin Holst Swende, with enhancements for coverage-guided prioritization.
