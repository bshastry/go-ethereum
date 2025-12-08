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

**Note:** Use absolute paths for `FUZZ_SEED_DIR` and `FUZZ_CORPUS_DIR`. Go tests run from the package directory (`tests/fuzzers/statetest`), not the shell's current directory.

```bash
# Use absolute path (recommended)
FUZZ_SEED_DIR=/path/to/goevmlab/corpus go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/

# Or use shell expansion
FUZZ_SEED_DIR=$(pwd)/../goevmlab/corpus go test -cover -run=TestFuzzStateTestCustomMutator -v ./tests/fuzzers/statetest/
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
│  │  │  2. If cross-VM entry from another client: verify & continue        │││
│  │  │  3. Get current coverage: before := testing.Coverage()              │││
│  │  │  4. Strip stale _crossvm metadata, then mutate input                │││
│  │  │  5. Execute with timeout via cancellationTracer                     │││
│  │  │  6. Get new coverage: after := testing.Coverage()                   │││
│  │  │  7. If after > before:                                              │││
│  │  │     - Generate trace hash via ExecuteAndNormalize                   │││
│  │  │     - Save enhanced corpus entry with _crossvm metadata             │││
│  │  │     - Add mutated input to HIGH priority queue                      │││
│  │  │     - Add to splicing corpus                                        │││
│  │  │  8. Record stats (execs, crashes, timeouts, coverage, saved)        │││
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
 SEEDS: 54861  HP_QUEUE: 0  SPLICING: 31  SAVED: 31
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
║ Seeds:           54861                                            ║
║ HP queue:        0                                                ║
║ Splicing pool:   31                                               ║
║ Corpus saved:    31                                               ║
╠═══════════════════════════════════════════════════════════════════╣
║ Crashes found:   0                                                ║
║ Timeouts:        0                                                ║
╚═══════════════════════════════════════════════════════════════════╝

Coverage guidance stats:
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
    ├── enhanced_corpus/   # Coverage-finding inputs with _crossvm metadata
    └── crashes/           # Crash-inducing inputs and consensus divergences
```

## Cross-Client Differential Testing

The fuzzer includes trace normalization for cross-client consensus verification, designed for 6-VM differential testing (geth, nethermind, besu, erigon, revm, evmone).

### How It Works

1. **Trace Normalization**: Converts EVM traces to a canonical format matching goevmlab
2. **Trace Hashing**: MD5 hash of normalized traces + stateRoot for comparison
3. **Enhanced Corpus**: Coverage-finding inputs saved with `_crossvm` metadata
4. **Cross-VM Verification**: When loading corpus from another client, verify instead of mutate

### Canonical Trace Format

Normalized trace lines include only these fields (matching goevmlab defaults):
- `depth` (decimal)
- `pc` (decimal)
- `section` (decimal, omitted if 0) - EOF only
- `functionDepth` (decimal, omitted if 0) - EOF only
- `gas` (decimal)
- `op` (hex, 0x-prefixed, 2 digits, zero-padded)
- `opName` (string)
- `stack` (array of hex strings, last 6 items only, minimal representation)

Example:
```json
{"depth":1,"pc":0,"gas":100000,"op":"0x60","opName":"PUSH1","stack":[]}
{"depth":1,"pc":2,"gas":99997,"op":"0x60","opName":"PUSH1","stack":["0x2"]}
{"stateRoot":"0x1234..."}
```

The hash is computed as: `MD5(trace_line_1 + "\n" + trace_line_2 + "\n" + ... + stateRoot_line + "\n")`

### Cross-VM Metadata Format (`_crossvm`)

When inputs discover new coverage, they are saved to `testdata/enhanced_corpus/` with embedded metadata:

```json
{
  "testName": {
    "env": {...},
    "pre": {...},
    "transaction": {...},
    "post": {...}
  },
  "_crossvm": {
    "traceHash": "13251158d97ea2420d51501e32f818fc",
    "stateRoot": "0x60a3fe53c5486f7967c947766fce4e08a7ebd8f3...",
    "traceLines": 7,
    "generatedBy": "geth",
    "version": "dev",
    "generatedAt": "2025-12-08T10:50:16Z"
  }
}
```

### Cross-VM Verification Mode

When the fuzzer encounters a corpus entry with `_crossvm` metadata from another client:

1. **Detect**: Check if `_crossvm.generatedBy != "geth"`
2. **Verify**: Execute the test with normalized tracing (no mutation)
3. **Compare**: Check if computed `traceHash` matches `_crossvm.traceHash`
4. **Report**: If hashes differ, save divergence to `testdata/crashes/`

This enables automatic detection of consensus bugs when running against a shared corpus.

### Divergence Reporting

When consensus divergences are detected:

1. **Test input saved**: `testdata/crashes/divergence_N.json`
2. **Log entry appended**: `testdata/crashes/crossvm_divergences.log`

Log format:
```
[2024-01-15T12:00:00Z] CONSENSUS DIVERGENCE #1: source=besu expected=abc123... actual=def456...
```

Progress/final reports include cross-VM stats:
```
CROSS-VM: verified=150 divergences=2
```

### Implementing Cross-VM Support in Other Clients

Other Ethereum clients can participate in cross-VM differential testing by:

1. Implementing the same canonical trace format (field order, hex formatting)
2. Including stateRoot as the final line in hash computation
3. Reading `_crossvm` metadata from corpus entries
4. Verifying entries from other clients instead of mutating them
5. Saving discovered inputs with `"generatedBy": "clientname"`

## Trace Dump Mode (Debug)

When investigating cross-VM divergences, you can dump normalized traces to files for manual comparison with other clients.

### Usage (Programmatic)

```go
import "github.com/ethereum/go-ethereum/tests/fuzzers/statetest"

config := &statetest.DumpTraceConfig{
    OutputPath:      "geth_trace.jsonl",
    IncludeFiltered: true,  // Include filtered entries (STOP, depth=0, duplicates)
}
result, err := statetest.ExecuteAndDumpTrace(testJSON, 5*time.Second, config)
```

### Output Format (JSONL)

```jsonl
{"_meta":{"client":"geth","version":"dev","fork":"London","inputHash":"abc123...","testName":"Test_d0g0v0","timestamp":"2025-12-08T12:00:00Z","normalizerVersion":"1"}}
{"depth":1,"pc":0,"gas":100000000,"op":"0x60","opName":"PUSH1","stack":[]}
{"depth":1,"pc":2,"gas":99999997,"op":"0x01","opName":"ADD","stack":["0x1"]}
{"_filtered":{"reason":"STOP_OPCODE","depth":1,"pc":100,"op":"0x00","opName":"STOP"}}
{"stateRoot":"0x123..."}
{"_result":{"traceHash":"abc123...","traceLines":42,"finalLineHash":"xyz789..."}}
```

### Metadata Fields (`_meta`)

| Field | Description |
|-------|-------------|
| `client` | Client name ("geth") |
| `version` | Client version string |
| `fork` | Ethereum fork name (London, Paris, Prague, etc.) |
| `inputHash` | MD5 hash of input test JSON |
| `testName` | Name of the specific subtest |
| `timestamp` | ISO 8601 timestamp |
| `normalizerVersion` | Version of normalizer algorithm (currently "1") |

### Filter Reason Codes (`_filtered`)

| Code | Description |
|------|-------------|
| `STOP_OPCODE` | STOP opcode (0x00) filtered |
| `DEPTH_ZERO` | depth=0 entry filtered (pre-execution) |
| `DUPLICATE` | Duplicate PC+depth+functionDepth entry filtered |

### Result Fields (`_result`)

| Field | Description |
|-------|-------------|
| `traceHash` | MD5 hash of all trace lines + stateRoot |
| `traceLines` | Number of trace lines included in hash |
| `finalLineHash` | MD5 hash of just the last line (helps identify where divergence occurred) |

### Comparing Traces Between Clients

```bash
# Generate traces with both clients
./geth-statetest --dump-trace=geth_trace.jsonl test.json
./besu-evmtool state-test --dump-trace=besu_trace.jsonl test.json

# Quick diff (ignoring metadata lines)
diff <(grep -v '_meta\|_result' geth_trace.jsonl) <(grep -v '_meta\|_result' besu_trace.jsonl)

# Find first divergent line
diff geth_trace.jsonl besu_trace.jsonl | head -20
```

### Implementing Trace Dump in Other Clients

To participate in cross-VM debugging:

1. Implement the `--dump-trace=<path>` flag
2. Output JSONL format with the same field order
3. Include `_meta` header with normalizer version
4. Optionally support `--dump-filtered` for filtered entry logging
5. Include `_result` footer with traceHash and finalLineHash

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
