# Generator Integration for StateTest Fuzzer

This document describes the generator integration framework that enables A/B testing between corpus+mutation fuzzing and semantic test generation using goevmlab factories.

## Overview

The statetest fuzzer now supports three input provider modes:

| Mode | Description | Use Case |
|------|-------------|----------|
| `mutation` | Corpus-based with 15 mutation strategies | Baseline fuzzing |
| `generation` | Semantic generators from goevmlab | Targeted feature testing |
| `hybrid` | Weighted mix of mutation + generation | Best coverage |

## Quick Start

```bash
# Default mutation-only (no extra deps)
go test -run TestFuzzStateTestAB ./tests/fuzzers/statetest/

# Pure generation mode (requires goevmlab)
FUZZ_PROVIDER=generation go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/

# Hybrid mode with 70% mutation / 30% generation
FUZZ_PROVIDER=hybrid FUZZ_MUTATION_RATIO=0.7 go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/
```

## Build Tags

The generator integration uses build tags to make goevmlab an optional dependency:

- **Without `-tags=generators`**: Only mutation provider available, no goevmlab dependency
- **With `-tags=generators`**: All providers available, requires goevmlab

## Environment Variables

### Provider Selection

| Variable | Values | Default | Description |
|----------|--------|---------|-------------|
| `FUZZ_PROVIDER` | `mutation`, `generation`, `hybrid` | `mutation` | Input provider mode |
| `FUZZ_FORK` | `Prague`, `Cancun`, etc. | `Prague` | Target fork for generators |

### Hybrid Mode Configuration

| Variable | Values | Default | Description |
|----------|--------|---------|-------------|
| `FUZZ_MUTATION_RATIO` | `0.0` - `1.0` | `0.7` | Ratio of mutation inputs (vs generation) |
| `FUZZ_ADAPTIVE_RATIO` | `true`, `false` | `false` | Auto-adjust ratio based on coverage |

### Generator Selection

| Variable | Example | Default | Description |
|----------|---------|---------|-------------|
| `FUZZ_GENERATORS` | `ecrecover,blake,bn254` | (all) | Comma-separated generator names |

## Available Generators

All 14 goevmlab semantic generators are available:

| Generator | Description | Fork Support |
|-----------|-------------|--------------|
| `ecrecover` | ECRECOVER precompile tests | All forks |
| `naive` | Random bytecode generation | All forks |
| `blake` | BLAKE2F precompile (EIP-152) | Istanbul+ |
| `bls` | BLS12-381 precompile (EIP-2537) | Prague+ |
| `bn254` | BN254 precompile tests | All forks |
| `precompiles` | All precompile coverage | All forks |
| `simpleops` | Simple opcode sequences | All forks |
| `memops` | Memory operations | All forks |
| `sstore_sload` | Storage operations | All forks |
| `tstore_tload` | Transient storage (EIP-1153) | Cancun+ |
| `auth` | AUTH/AUTHCALL (EIP-3074) | Prague+ |
| `kzg` | KZG point evaluation | Cancun+ |
| `p256` | P256VERIFY precompile | Prague+ |
| `modexp` | MODEXP gas edge cases | All forks |

## Architecture

```
┌─────────────────────────────────────────────────────┐
│                  TestFuzzStateTestAB                │
│              (fuzzer_test.go entry point)           │
└─────────────────┬───────────────────────────────────┘
                  │
                  ▼
┌─────────────────────────────────────────────────────┐
│                   InputProvider                     │
│                   (provider.go)                     │
│                                                     │
│   interface {                                       │
│       Next() (data []byte, source string, error)   │
│       Feedback(data, source, coverageDelta)        │
│       Stats() ProviderStats                        │
│       Name() string                                │
│   }                                                │
└───────┬─────────────────┬─────────────────┬────────┘
        │                 │                 │
        ▼                 ▼                 ▼
┌───────────────┐ ┌───────────────┐ ┌───────────────┐
│   Mutation    │ │   Generator   │ │    Hybrid     │
│   Provider    │ │   Provider    │ │   Provider    │
└───────────────┘ └───────────────┘ └───────────────┘
        │                 │                 │
        ▼                 ▼                 │
┌───────────────┐ ┌───────────────┐         │
│ RawMutator    │ │ goevmlab      │◄────────┘
│ (15 strategies)│ │ factories    │
└───────────────┘ └───────────────┘
```

## A/B Testing

The framework tracks per-source statistics to compare effectiveness:

```
╔═══════════════════════════════════════════════════════════════════╗
║                    STATETEST FUZZER REPORT                       ║
╠═══════════════════════════════════════════════════════════════════╣
║ Provider:        hybrid                                           ║
║ Duration:        60s                                              ║
║ Total execs:     45230                                            ║
║ Exec rate:       754/sec                                          ║
╠═══════════════════════════════════════════════════════════════════╣
║                       SOURCE BREAKDOWN                            ║
╠═══════════════════════════════════════════════════════════════════╣
║ mutation:storage     inputs=3421     finds=12     rate=0.351% ║
║ mutation:bytecode    inputs=2891     finds=8      rate=0.277% ║
║ generation:bn254     inputs=1205     finds=15     rate=1.245% ║
║ generation:bls       inputs=987      finds=11     rate=1.114% ║
╚═══════════════════════════════════════════════════════════════════╝
```

## Cross-Feeding

When a generator produces an input that increases coverage, it's automatically added to the mutation corpus. This enables:

1. Generator creates semantically valid test case
2. Mutator explores variations of that structure
3. Combined exploration of semantic + structural space

## File Structure

```
tests/fuzzers/statetest/
├── provider.go              # InputProvider interface
├── mutation_provider.go     # Wraps existing mutation flow
├── provider_stub.go         # Stub for non-generator builds
├── provider_generators.go   # Generator/hybrid factory (build-tagged)
├── hybrid_provider.go       # Combines mutation + generation
├── config.go                # Environment variable helpers
└── generators/
    ├── generator.go         # Generator interface
    ├── registry.go          # Generator registry with fork awareness
    ├── goevmlab_adapter.go  # Adapts goevmlab factories
    ├── provider.go          # GeneratorProvider implementation
    └── stub.go              # Stub registry for non-generator builds
```

## Example Usage

### Run A/B comparison

```bash
# Test mutation-only baseline
FUZZ_DURATION=5m FUZZ_PROVIDER=mutation \
  go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/ -v

# Test generation-only
FUZZ_DURATION=5m FUZZ_PROVIDER=generation \
  go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/ -v

# Test hybrid with various ratios
for ratio in 0.3 0.5 0.7 0.9; do
  FUZZ_DURATION=5m FUZZ_PROVIDER=hybrid FUZZ_MUTATION_RATIO=$ratio \
    go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/ -v
done
```

### Focus on specific precompile

```bash
# Only test BLS generators
FUZZ_GENERATORS=bls FUZZ_PROVIDER=generation \
  go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/
```

### Adaptive ratio (auto-adjusts based on coverage)

```bash
FUZZ_PROVIDER=hybrid FUZZ_ADAPTIVE_RATIO=true \
  go test -tags=generators -run TestFuzzStateTestAB ./tests/fuzzers/statetest/
```

## Integration with goevmlab

This integration uses goevmlab's `fuzzing.Factory` function which provides semantic test generation for specific EVM features. The factory returns a function that generates `StFiller` objects which are then converted to state test JSON format.

To use this feature with a local goevmlab checkout:

```bash
# Create go.work to use local goevmlab
cat > go.work << EOF
go 1.24.0

use (
    .
    ../goevmlab
)
EOF
```
