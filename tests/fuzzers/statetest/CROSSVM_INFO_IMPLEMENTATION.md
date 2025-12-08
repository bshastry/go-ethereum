# Cross-VM Info Implementation

This document describes the implementation of the EEST-standard `_info` field for cross-VM consensus verification in the statetest fuzzer.

## Specification

The implementation follows the [CROSSVM_INFO_SPEC.md](../../../CROSSVM_INFO_SPEC.md) specification which defines how to embed cross-VM consensus verification metadata in Ethereum state test JSON files.

## Key Changes

### 1. EEST Standard Format

Metadata is now placed inside the `_info` field within each test object, following the official EEST (Ethereum Execution Spec Tests) convention:

```json
{
  "testName": {
    "_info": {
      "comment": "Cross-VM consensus verification test",
      "generatedBy": "geth",
      "traceHash": "abc123def456...",
      "stateRoot": "0x1234...",
      "crossvmVersion": "1.0",
      "traceLines": 42,
      "generatedAt": "2025-12-08T12:00:00Z",
      "version": "dev"
    },
    "env": { ... },
    "pre": { ... },
    "transaction": { ... },
    "post": { ... }
  }
}
```

### 2. Why This Matters

The legacy format placed `_crossvm` at the top level:
```json
{
  "testName": { ... },
  "_crossvm": { ... }    // Gets parsed as a broken test entry!
}
```

This caused problems because Ethereum clients treat all top-level keys as test names. The EEST standard places metadata inside test objects where clients ignore unknown fields.

### 3. Modified Files

- **corpus_saver.go**: Updated `SaveEnhancedCorpusEntry()` to inject `_info` inside each test object
- **fuzzer_test.go**: Updated `extractCrossVMMetadata()` to read from `_info` inside test objects
- **corpus_saver_test.go**: Comprehensive unit tests for EEST format compliance

### 4. Backward Compatibility

The implementation maintains full backward compatibility:
- **Reading**: Supports both EEST standard (`_info` inside test objects) and legacy (`_crossvm` at top level) formats
- **Writing**: Always writes in EEST standard format
- **Stripping**: Removes metadata from both formats

### 5. Metadata Fields

| Field | Required | Description |
|-------|----------|-------------|
| `comment` | No | Human-readable description |
| `generatedBy` | **Yes** | Which VM generated this: geth, nethermind, besu, erigon, revm, evmone |
| `traceHash` | **Yes** | MD5 hash of normalized trace + stateRoot |
| `stateRoot` | **Yes** | Expected post-execution state root |
| `crossvmVersion` | **Yes** | Schema version: "1.0" |
| `traceLines` | No | Number of trace lines |
| `generatedAt` | No | ISO 8601 timestamp |
| `version` | No | VM version string |
| `fork` | No | Fork name (e.g., "Cancun", "Prague") |

### 6. Usage

#### Generating Cross-VM Corpus

```bash
# Run the fuzzer to generate EEST-compliant corpus entries
FUZZ_DURATION=1h FUZZ_CORPUS_DIR=./corpus go test -cover \
  -run=TestFuzzStateTestCustomMutator ./tests/fuzzers/statetest/ -timeout=2h
```

#### Verifying Cross-VM Entries

When loading corpus entries from another VM (e.g., Nethermind), geth will:
1. Parse the `_info` field from inside test objects
2. Check if `generatedBy` differs from "geth"
3. Execute the test with trace normalization
4. Compare the computed `traceHash` with the stored value
5. Log any consensus divergences

### 7. Tests

Run the unit tests:
```bash
go test -v ./tests/fuzzers/statetest/ -run "Test.*EEST|Test.*CrossVM"
```

## Version History

- **1.0** (2025-12-08): Initial EEST-compliant implementation
