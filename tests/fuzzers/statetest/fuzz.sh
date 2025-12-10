#!/bin/bash
# Coverage-guided state test fuzzer with sensible defaults
#
# Usage:
#   ./fuzz.sh                     # Basic 2-minute run
#   ./fuzz.sh --ab                # A/B testing mode (mutation vs generation)
#   ./fuzz.sh --duration 1h       # Extended run
#   ./fuzz.sh --workers 32        # Custom worker count
#   ./fuzz.sh --seed-dir /path    # Custom seed corpus
#
# Environment variables are also supported (FUZZ_DURATION, FUZZ_WORKERS, etc.)

set -e

# Default coverage packages for meaningful EVM fuzzing
# Includes geth core packages + external crypto libraries used by precompiles
DEFAULT_COVERPKG="github.com/ethereum/go-ethereum/core/vm,\
github.com/ethereum/go-ethereum/core/state,\
github.com/ethereum/go-ethereum/core,\
github.com/ethereum/go-ethereum/core/types,\
github.com/ethereum/go-ethereum/crypto,\
github.com/ethereum/go-ethereum/crypto/bn256,\
github.com/ethereum/go-ethereum/crypto/blake2b,\
github.com/ethereum/go-ethereum/crypto/kzg4844,\
github.com/ethereum/go-ethereum/crypto/secp256k1,\
github.com/consensys/gnark-crypto/ecc/bls12-381,\
github.com/consensys/gnark-crypto/ecc/bls12-381/fp,\
github.com/consensys/gnark-crypto/ecc/bls12-381/fr,\
github.com/consensys/gnark-crypto/ecc/bn254,\
github.com/consensys/gnark-crypto/ecc/bn254/fp,\
github.com/consensys/gnark-crypto/ecc/bn254/fr"

# Defaults
DURATION="${FUZZ_DURATION:-2m}"
WORKERS="${FUZZ_WORKERS:-$(nproc)}"
SEED_DIR="${FUZZ_SEED_DIR:-}"
CORPUS_DIR="${FUZZ_CORPUS_DIR:-}"
FORK="${FUZZ_FORK:-Osaka}"
PROVIDER="${FUZZ_PROVIDER:-mutation}"
MUTATION_RATIO="${FUZZ_MUTATION_RATIO:-0.5}"
COVERPKG="${FUZZ_COVERPKG:-$DEFAULT_COVERPKG}"
AB_MODE=false
VERBOSE="-v"

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --ab)
            AB_MODE=true
            PROVIDER="hybrid"
            shift
            ;;
        --duration)
            DURATION="$2"
            shift 2
            ;;
        --workers)
            WORKERS="$2"
            shift 2
            ;;
        --seed-dir)
            SEED_DIR="$2"
            shift 2
            ;;
        --corpus-dir)
            CORPUS_DIR="$2"
            shift 2
            ;;
        --fork)
            FORK="$2"
            shift 2
            ;;
        --ratio)
            MUTATION_RATIO="$2"
            shift 2
            ;;
        --quiet)
            VERBOSE=""
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [options]"
            echo ""
            echo "Options:"
            echo "  --ab              Enable A/B testing mode (mutation vs generation)"
            echo "  --duration TIME   Fuzzing duration (default: 2m)"
            echo "  --workers N       Number of parallel workers (default: nproc)"
            echo "  --seed-dir PATH   Path to seed corpus"
            echo "  --corpus-dir PATH Path to output corpus"
            echo "  --fork NAME       Target fork (default: Osaka)"
            echo "  --ratio FLOAT     Mutation ratio for hybrid mode (default: 0.5)"
            echo "  --quiet           Suppress verbose output"
            echo ""
            echo "Environment variables:"
            echo "  FUZZ_DURATION, FUZZ_WORKERS, FUZZ_SEED_DIR, FUZZ_CORPUS_DIR"
            echo "  FUZZ_FORK, FUZZ_PROVIDER, FUZZ_MUTATION_RATIO, FUZZ_COVERPKG"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Calculate timeout (duration + 10 minutes buffer)
TIMEOUT_SECS=$(echo "$DURATION" | sed 's/h/*3600+/g; s/m/*60+/g; s/s/+/g; s/+$//' | bc)
TIMEOUT_SECS=$((TIMEOUT_SECS + 600))

# Build command
CMD="go test -cover -coverpkg=$COVERPKG"

if $AB_MODE; then
    CMD="$CMD -tags=generators -run=TestFuzzStateTestAB"
else
    CMD="$CMD -run=TestFuzzStateTestCustomMutator"
fi

CMD="$CMD $VERBOSE ./tests/fuzzers/statetest/ -timeout=${TIMEOUT_SECS}s"

# Set environment
export FUZZ_DURATION="$DURATION"
export FUZZ_WORKERS="$WORKERS"
export FUZZ_FORK="$FORK"
export FUZZ_PROVIDER="$PROVIDER"
export FUZZ_MUTATION_RATIO="$MUTATION_RATIO"

if [[ -n "$SEED_DIR" ]]; then
    export FUZZ_SEED_DIR="$SEED_DIR"
fi

if [[ -n "$CORPUS_DIR" ]]; then
    export FUZZ_CORPUS_DIR="$CORPUS_DIR"
fi

# Print configuration
echo "=== Coverage-Guided State Test Fuzzer ==="
echo "Duration:    $DURATION"
echo "Workers:     $WORKERS"
echo "Fork:        $FORK"
echo "Provider:    $PROVIDER"
if $AB_MODE; then
    echo "Mut Ratio:   $MUTATION_RATIO"
fi
echo "Seed Dir:    ${SEED_DIR:-<default>}"
echo "Corpus Dir:  ${CORPUS_DIR:-<default>}"
echo "Coverage:    EVM packages (core/vm, core/state, crypto, ...)"
echo "=========================================="
echo ""

# Run
exec $CMD
