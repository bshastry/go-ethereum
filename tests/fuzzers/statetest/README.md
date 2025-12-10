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
# Using the convenience script (recommended - includes EVM coverage by default)
./tests/fuzzers/statetest/fuzz.sh

# Or manually
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

### Coverage-Guided Fuzzing (Recommended)

**Important:** By default, `-cover` only tracks coverage of the fuzzer package itself. To track meaningful EVM coverage, use `-coverpkg` to include the packages being fuzzed:

```bash
# Recommended packages for EVM state test fuzzing
COVERPKG="github.com/ethereum/go-ethereum/core/vm,\
github.com/ethereum/go-ethereum/core/state,\
github.com/ethereum/go-ethereum/core,\
github.com/ethereum/go-ethereum/core/types,\
github.com/ethereum/go-ethereum/crypto"

# Full command with EVM coverage tracking
FUZZ_DURATION=1h FUZZ_WORKERS=22 \
FUZZ_SEED_DIR=$(pwd)/../goevmlab/corpus \
FUZZ_CORPUS_DIR=$(pwd)/out-$(date -I) \
go test -cover -coverpkg=$COVERPKG \
  -run=TestFuzzStateTestCustomMutator -v \
  ./tests/fuzzers/statetest/ -timeout=2h
```

**Package coverage guide:**

| Package | What it covers |
|---------|---------------|
| `core/vm` | EVM interpreter, opcodes, precompiles, gas metering |
| `core/state` | State database, account storage, trie operations |
| `core` | Block processing, state transitions, consensus |
| `core/types` | Transaction types, receipts, logs |
| `crypto` | ECDSA, keccak256, secp256k1, bn256 precompiles |

**Additional packages for specific testing:**

```bash
# For blob transaction testing (Cancun+)
COVERPKG="$COVERPKG,github.com/ethereum/go-ethereum/crypto/kzg4844"

# For precompile testing (geth's wrappers)
COVERPKG="$COVERPKG,github.com/ethereum/go-ethereum/crypto/bn256,github.com/ethereum/go-ethereum/crypto/blake2b,github.com/ethereum/go-ethereum/crypto/secp256k1"
```

**External crypto packages:** Go 1.20+ supports instrumenting external dependencies. The `fuzz.sh` script automatically includes key crypto libraries:
- `gnark-crypto/ecc/bls12-381` - BLS12-381 precompiles (EIP-2537)
- `gnark-crypto/ecc/bn254` - BN254/alt_bn128 precompiles (ecAdd, ecMul, ecPairing)

### A/B Testing Mode (Mutation vs Generation)

For comparing mutation-based and generation-based fuzzing effectiveness:

```bash
# Using convenience script
./tests/fuzzers/statetest/fuzz.sh --ab --duration 1h --seed-dir $(pwd)/../goevmlab/corpus

# Or manually
FUZZ_DURATION=1h FUZZ_MUTATION_RATIO=0.5 FUZZ_FORK=Osaka \
FUZZ_SEED_DIR=$(pwd)/../goevmlab/corpus \
FUZZ_CORPUS_DIR=$(pwd)/out-$(date -I) \
FUZZ_WORKERS=22 FUZZ_PROVIDER=hybrid \
go test -cover -coverpkg=$COVERPKG \
  -tags=generators -run=TestFuzzStateTestAB \
  ./tests/fuzzers/statetest/ -timeout=2h -v
```

### Convenience Script Reference

The `fuzz.sh` script provides sensible defaults and automatically includes EVM coverage packages:

```bash
./fuzz.sh                              # Basic 2-minute run with EVM coverage
./fuzz.sh --ab                         # A/B testing mode
./fuzz.sh --duration 1h --workers 32   # Extended run
./fuzz.sh --seed-dir /path/to/corpus   # Custom seed corpus
./fuzz.sh --help                       # Show all options
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `FUZZ_DURATION` | `2m` | How long to run the fuzzer |
| `FUZZ_WORKERS` | `NumCPU` | Number of parallel workers |
| `FUZZ_SEED_DIR` | `testdata/seeds` | Directory containing seed JSON files |
| `FUZZ_STRATEGY` | `combined` | Mutation strategy to use |
| `FUZZ_CORPUS_DIR` | `testdata/enhanced_corpus` | Output directory for trace-enhanced corpus entries |
| `FUZZ_ADAPTIVE_HP` | `false` | Enable adaptive HP probability (opt-in) |
| `FUZZ_ADAPTIVE_HP_MIN` | `0.2` | Minimum HP probability bound |
| `FUZZ_ADAPTIVE_HP_MAX` | `0.95` | Maximum HP probability bound |
| `FUZZ_ADAPTIVE_DROUGHT_SEC` | `30` | Drought detection threshold in seconds |

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

## Architecture Deep Dive: Queues and Retention

The fuzzer maintains three distinct data structures that work together to maximize coverage exploration efficiency. Understanding their interaction is key to understanding the fuzzer's behavior.

### The Three Queues

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                         CoverageCorpus Structure                             │
├─────────────────────────────────────────────────────────────────────────────┤
│                                                                             │
│  1. NORMAL SEEDS ([][]byte)                                                 │
│     ─────────────────────────                                               │
│     • Original corpus (e.g., goevmlab 54,861 tests)                         │
│     • Round-robin access via seedIndex                                      │
│     • Never modified during fuzzing                                         │
│     • Used when HP queue is empty or RNG selects seeds                      │
│                                                                             │
│  2. HIGH PRIORITY QUEUE ([]*PriorityInput)                                  │
│     ─────────────────────────────────────────                               │
│     • Coverage-finding inputs with retention                                │
│     • Weighted random selection by priority                                 │
│     • Items STAY after being picked (retention model)                       │
│     • Priority decays with each pick                                        │
│     • Items culled when exhausted (low priority + enough picks)             │
│     • O(1) lookup via SHA256 hash index for deduplication                   │
│                                                                             │
│  3. SPLICING POOL ([][]byte)                                                │
│     ─────────────────────────                                               │
│     • All coverage-finding inputs (permanent)                               │
│     • Used by splicing mutation strategy                                    │
│     • 70% weight in donor selection (30% from seeds)                        │
│     • Max size 10,000 (random replacement when full)                        │
│                                                                             │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Input Selection Flow

When `Pop()` is called:

```
┌───────────────────────────────────────────────────────────────────────────┐
│                            Pop() Decision Tree                             │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  1. RNG roll: r < highPriorityP (default 80%)?                            │
│     │                                                                     │
│     ├── YES ─┬── HP queue empty? ──────────────────┬── YES ──► Seeds      │
│     │        │                                     │                      │
│     │        └── NO ─► Weighted random selection ──┘                      │
│     │              from HP queue by priority                              │
│     │                                                                     │
│     └── NO ──────────────────────────────────────────────────► Seeds      │
│                                                                           │
│  Result: Either HP item (with decay applied) or seed (round-robin)        │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Retention Model

Unlike traditional consume-once queues, the HP queue implements **retention with decay**:

```
┌───────────────────────────────────────────────────────────────────────────┐
│                         PriorityInput Lifecycle                            │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  DISCOVERY                                                                │
│  ───────────                                                              │
│  When input finds new coverage:                                           │
│    Priority = coverageDelta × 1,000,000                                   │
│    BasePriority = Priority (stored for reference)                         │
│    PickCount = 0                                                          │
│                                                                           │
│  ON EACH PICK                                                             │
│  ─────────────                                                            │
│    PickCount++                                                            │
│    Priority = Priority × decayRate (default 0.99)                         │
│    Priority = max(Priority, minPriority) (floor at 1)                     │
│                                                                           │
│  CULLING (every cullInterval=1000 pops)                                   │
│  ─────────────────────────────────────                                    │
│    Item is culled if BOTH conditions are true:                            │
│      1. Priority ≤ minPriority (1)                                        │
│      2. PickCount ≥ minPicksToCull (10)                                   │
│                                                                           │
│  EXAMPLE LIFECYCLE                                                        │
│  ─────────────────                                                        │
│    coverageDelta=0.0003 → Priority=300                                    │
│    After 50 picks: 300 × 0.99^50 = 182                                    │
│    After 200 picks: 300 × 0.99^200 = 40                                   │
│    After 500 picks: 300 × 0.99^500 = 2 → eligible for culling             │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Culling Math

With default parameters:
- `decayRate = 0.99` (1% priority reduction per pick)
- `minPriority = 1` (floor value)
- `minPicksToCull = 10` (minimum picks before eligible)

**Formula**: Picks to reach minPriority from initial priority P:
```
n = log(minPriority / P) / log(decayRate)
n = log(1 / P) / log(0.99)

Examples:
  P=300   (typical delta=0.0003): ~568 picks to reach threshold
  P=1000  (delta=0.001):          ~688 picks to reach threshold
  P=10000 (delta=0.01):           ~918 picks to reach threshold
```

The slow decay ensures high-value inputs get many mutation attempts before being culled, while items that stop producing new coverage are eventually removed to keep the queue fresh.

### Weighted Random Selection

Items are selected with probability proportional to their current priority:

```
┌───────────────────────────────────────────────────────────────────────────┐
│                      Weighted Selection Algorithm                          │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  Running sum: totalPriority = Σ(item.Priority) for all items              │
│                                                                           │
│  Selection:                                                               │
│    1. target = random(0, totalPriority)                                   │
│    2. cumulative = 0                                                      │
│    3. For each item:                                                      │
│         cumulative += item.Priority                                       │
│         if cumulative > target: return item                               │
│                                                                           │
│  Complexity: O(n) selection, O(1) totalPriority maintenance               │
│                                                                           │
│  EXAMPLE                                                                  │
│  ───────                                                                  │
│  Queue: [A(p=100), B(p=300), C(p=50)]                                     │
│  totalPriority = 450                                                      │
│                                                                           │
│  Selection probabilities:                                                 │
│    A: 100/450 = 22%                                                       │
│    B: 300/450 = 67%                                                       │
│    C: 50/450  = 11%                                                       │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Splice Donor Selection

The splicing strategy needs "donor" inputs to combine with the current input:

```
┌───────────────────────────────────────────────────────────────────────────┐
│                        Splice Donor Selection                              │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  GetRandomInput() called by SplicingStrategy:                             │
│                                                                           │
│    ┌─── RNG roll < 70? ───┐                                               │
│    │                      │                                               │
│    │  YES                 │  NO                                           │
│    │   │                  │   │                                           │
│    ▼   │                  ▼   │                                           │
│  splicingPool         normalSeeds                                         │
│  (coverage-finders)   (expert EEST tests)                                 │
│                                                                           │
│  RATIONALE                                                                │
│  ─────────                                                                │
│  • 70% coverage pool: Inputs that found new paths                         │
│  • 30% seeds: EEST tests with expert-designed edge cases                  │
│  • Keeps EVM-specific edge cases in play even after                       │
│    many coverage-finding inputs have been discovered                      │
│                                                                           │
│  TRACKING                                                                 │
│  ────────                                                                 │
│  UI displays: SPLICE_DONORS: coverage=N (X%) seeds=M (Y%)                 │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### How Queues Work Together

```
┌───────────────────────────────────────────────────────────────────────────┐
│                     Mutation → Execution → Feedback                        │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  1. POP: Get base input                                                   │
│     │                                                                     │
│     ├── 80%: HP queue (weighted by priority)                              │
│     │        Item stays in queue, priority decays                         │
│     │                                                                     │
│     └── 20%: Seeds (round-robin)                                          │
│              Original corpus cycling                                      │
│                                                                           │
│  2. MUTATE: Apply mutation strategy                                       │
│     │                                                                     │
│     └── If splicing strategy selected:                                    │
│         Call GetRandomInput() for donor                                   │
│         ├── 70%: From splicingPool (coverage-finders)                     │
│         └── 30%: From normalSeeds (EEST expert tests)                     │
│                                                                           │
│  3. EXECUTE: Run mutated input                                            │
│     │                                                                     │
│     └── Measure coverage delta                                            │
│                                                                           │
│  4. FEEDBACK: If new coverage found                                       │
│     │                                                                     │
│     ├── Add to HP queue (priority = delta × 1,000,000)                    │
│     │   └── If duplicate: boost existing item's priority instead          │
│     │                                                                     │
│     └── Add to splicingPool (permanent, for future donors)                │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Understanding the UI Metrics

#### Last Coverage Find (LAST field)

The `LAST` field shows when and which strategy most recently improved coverage:

```
LAST: 12s (mutation:bytecode)   # 12 seconds ago via bytecode mutation
LAST: 5s (generation:ecrecover) # 5 seconds ago via ecrecover generator
LAST: never (none)              # No coverage finds yet
```

**Format:** `LAST: <duration> (<strategy>)`

**Strategy names:**
- Mutation strategies: `mutation:bytecode`, `mutation:havoc`, `mutation:splicing`, `mutation:original`, etc.
- Generation strategies: `generation:ecrecover`, `generation:bn254`, `generation:bls`, etc.

**Use cases:**
- **Diagnose stalls**: When coverage growth slows, see which strategy was last productive
- **Validate strategies**: Confirm that specific strategies (e.g., `generation:ecrecover`) are working
- **A/B testing feedback**: Real-time indication of which approach is "hot"

The time and strategy are updated atomically to ensure consistency in multi-worker scenarios.

#### Corpus Statistics

```
CORPUS: seeds=54861 hp_q=1142(added=1159) splice=1142 | HP_PICKS: 18234 (80.1%) SEED_PICKS: 4521
RETENTION: avg_picks=16.0 culled=17 total_priority=285420
SPLICE_DONORS: coverage=8234 (70.2%) seeds=3490 (29.8%)
```

| Metric | Source | Meaning |
|--------|--------|---------|
| `seeds=54861` | `len(normalSeeds)` | Original corpus size |
| `hp_q=1142` | `len(hpQueue)` | Current HP queue size |
| `added=1159` | `totalInputsAdded` | Total ever added to HP queue |
| `splice=1142` | `len(splicingPool)` | Splicing donor pool size |
| `HP_PICKS: 18234 (80.1%)` | `hpPicks` | Times HP queue was selected |
| `SEED_PICKS: 4521` | `seedPicks` | Times seeds were selected |
| `avg_picks=16.0` | Computed | Average PickCount across HP items |
| `culled=17` | `totalCulled` | Items removed from HP queue |
| `total_priority=285420` | `totalPriority` | Sum of all priorities (for weighted selection) |
| `coverage=8234 (70.2%)` | `spliceCoverageDonors` | Splice donors from coverage pool |
| `seeds=3490 (29.8%)` | `spliceSeedDonors` | Splice donors from original seeds |

### Configuration Options

| Option | Default | Description |
|--------|---------|-------------|
| `decayRate` | 0.99 | Priority multiplier per pick (higher = slower decay) |
| `minPriority` | 1 | Floor value before culling eligible |
| `minPicksToCull` | 10 | Minimum picks before culling eligible |
| `cullInterval` | 1000 | Check for culling every N pops |
| `highPriorityP` | 0.8 | Base probability of selecting from HP queue |
| `maxQueueSize` | 10000 | Max HP queue size (0 = unlimited) |
| `maxSplicingLen` | 10000 | Max splicing pool size |

**Adaptive HP Options** (opt-in):

| Option | Default | Description |
|--------|---------|-------------|
| `adaptiveHP` | false | Enable adaptive HP probability adjustment |
| `adaptiveMinP` | 0.2 | Minimum probability bound |
| `adaptiveMaxP` | 0.95 | Maximum probability bound |
| `adaptiveDroughtThreshold` | 30s | Time without finds before reducing HP probability |
| `adaptiveWarmupPicks` | 500 | Picks before adaptation fully activates |

Configuration via functional options:
```go
corpus := NewCoverageCorpus(seeds,
    WithDecayRate(0.995),        // Even slower decay
    WithMinPriority(1),          // Keep default
    WithMinPicksToCull(20),      // Require more picks before culling
    WithHighPriorityProbability(0.9), // More HP-focused

    // Enable adaptive HP probability
    WithAdaptiveHP(true),
    WithAdaptiveBounds(0.2, 0.95),
    WithAdaptiveDroughtThreshold(30 * time.Second),
)
```

### Adaptive HP Probability

When enabled (`WithAdaptiveHP(true)`), the HP probability dynamically adjusts based on:

```
effectiveP = clamp(baseP + queueAdj + droughtAdj + successAdj, minP, maxP)
```

**Additive Factors:**

| Factor | Range | Description |
|--------|-------|-------------|
| `queueAdj` | [-0.30, +0.15] | Reduces probability when HP queue is empty/small, slight boost when full |
| `droughtAdj` | [-0.20, 0] | Reduces probability during coverage droughts to explore more seeds |
| `successAdj` | [-0.15, +0.15] | Adjusts toward whichever source (HP or seeds) has better efficiency |

**Queue Adjustment Curve:**
```
HP Queue Size | Adjustment
--------------|----------
0             | -0.30 (force more seeds)
1             | -0.20
10            | -0.10
100           | 0.00
1000          | +0.10
10000+        | +0.15
```

**Drought Adjustment Curve:**
```
Time Since Find | Adjustment
----------------|----------
<30s            | 0.00
1x threshold    | -0.05
2x threshold    | -0.10
5x threshold    | -0.15
10x threshold+  | -0.20 (floor)
```

**Success Adjustment:**
- Compares `hpEfficiency` vs `seedEfficiency` (finds per pick)
- Adjusts toward the more productive source
- Requires warmup period (500 picks) before activating

**Cold Start Phase-In:**
```go
confidence := min(1.0, totalPicks / warmupPicks)
adjustment = adjustment * confidence  // Gradually increase from 0 to full
```

This prevents sudden probability swings during the first few hundred picks when statistical data is insufficient.

### Source Statistics: Cumulative vs Per-Interval

**Important**: All source statistics (TOP_SOURCES, MUT/GEN totals, SOURCE BREAKDOWN) are **cumulative from the start of the run** - they are NOT reset between progress reports.

```
┌───────────────────────────────────────────────────────────────────────────┐
│                      How Source Stats Work                                 │
├───────────────────────────────────────────────────────────────────────────┤
│                                                                           │
│  RECORDING (happens during fuzzing)                                       │
│  ──────────────────────────────────                                       │
│                                                                           │
│  1. When Next() returns an input:                                         │
│     - source = "mutation:bytecode" or "generation:ecrecover" etc.         │
│     - recordInput(source) → increments sourceBreakdown[source].Inputs     │
│                                                                           │
│  2. When Feedback() is called with coverageDelta:                         │
│     - recordFeedback(source, delta)                                       │
│     - If delta > 0:                                                       │
│       → increments sourceBreakdown[source].CoverageFinds                  │
│       → adds delta to sourceBreakdown[source].TotalDelta                  │
│                                                                           │
│  DISPLAY (every 3 seconds)                                                │
│  ─────────────────────────                                                │
│                                                                           │
│  TOP_SOURCES: Shows top 3 by CoverageFinds (cumulative, since start)      │
│  MUT/GEN:     Shows aggregate totals [mutation_total] and [generation_total] │
│  SOURCE BREAKDOWN: Final report shows all sources with their stats        │
│                                                                           │
│  ALL STATS ARE CUMULATIVE - never reset during the run                    │
│                                                                           │
└───────────────────────────────────────────────────────────────────────────┘
```

### Comparing Mutation vs Generation Effectiveness

When running A/B tests (`--ab` mode), there are multiple metrics to compare effectiveness:

```
MUT: 45/12000 (0.38%)  GEN: 23/8000 (0.29%)
TOP_SOURCES: mutation:bytecode(15), generation:ecrecover(12), mutation:havoc(10)
```

**Key Metrics:**

| Metric | What It Measures | How to Use |
|--------|------------------|------------|
| **Find Rate** | `CoverageFinds / Inputs` | Higher = more efficient at finding new coverage per attempt |
| **Find Count** | Raw `CoverageFinds` | Higher = contributed more absolute coverage |
| **Total Delta** | Sum of coverage deltas | Higher = found more significant coverage |
| **Avg Delta per Find** | `TotalDelta / CoverageFinds` | Higher = each find is more valuable |

**Interpreting Results:**

1. **Find Rate** is the primary efficiency metric:
   ```
   MUT: 45/12000 (0.38%)  → 0.38% find rate
   GEN: 23/8000 (0.29%)   → 0.29% find rate
   ```
   Here mutation is more efficient (0.38% vs 0.29%).

2. **Find Count** shows absolute contribution:
   - If mutation has 45 finds and generation has 23, mutation contributed ~2x more coverage.
   - But consider input counts: mutation ran 12000 inputs, generation only 8000.

3. **TOP_SOURCES** shows which specific strategies are working:
   ```
   TOP_SOURCES: mutation:bytecode(15), generation:ecrecover(12), mutation:havoc(10)
   ```
   - `mutation:bytecode` is the top performer with 15 finds
   - `generation:ecrecover` (a generative strategy) is 2nd with 12 finds
   - Numbers in parentheses are cumulative find counts

4. **Final SOURCE BREAKDOWN** gives the complete picture:
   ```
   ╠═══════════════════════════════════════════════════════════════════╣
   ║                       SOURCE BREAKDOWN                            ║
   ╠═══════════════════════════════════════════════════════════════════╣
   ║ [mutation_total]     inputs=12000    finds=45     rate=0.375%    ║
   ║ [generation_total]   inputs=8000     finds=23     rate=0.288%    ║
   ║ mutation:bytecode    inputs=2400     finds=15     rate=0.625%    ║
   ║ generation:ecrecover inputs=1600     finds=12     rate=0.750%    ║
   ║ mutation:havoc       inputs=2000     finds=10     rate=0.500%    ║
   ```

   Look at the **rate column** for efficiency:
   - `generation:ecrecover` has 0.750% rate (highest!)
   - `mutation:bytecode` has 0.625% rate
   - Overall mutation has 0.375%, generation has 0.288%

**Recommendations:**

1. **For overall strategy comparison**: Use the aggregate `[mutation_total]` vs `[generation_total]` find rates
2. **For fine-tuning weights**: Look at individual source find rates in SOURCE BREAKDOWN
3. **For quick checks during run**: Watch TOP_SOURCES to see which strategies are currently winning
4. **For statistical significance**: Run for at least 10-15 minutes to get meaningful sample sizes

**Example Analysis:**

```
After 30 minutes:
MUT: 145/45000 (0.32%)  GEN: 89/35000 (0.25%)

Conclusion: Mutation is more efficient overall (0.32% vs 0.25%)

TOP_SOURCES: mutation:splicing(42), mutation:bytecode(38), generation:precompile(35)

Conclusion: Splicing is the most effective individual strategy.
            The precompile generator is competitive with top mutation strategies.

Recommendation: Consider increasing splicing weight in mutation mix,
                or creating more precompile-focused generators.
```

## Output Example

```
═══════════════════════════════════════════════════════════════════
 RUNTIME: 30s           WORKERS: 22    STRATEGY: combined
───────────────────────────────────────────────────────────────────
 EXEC: 186005 (6197/s)  CRASH: 0  TIMEOUT: 0
 COV:  31.646%  FINDS: 31  LAST: 27s (mutation:bytecode)  GROWTH: +0.0000%/min
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

## Standalone Trace Tool

The `statetest-trace` CLI tool processes a corpus of state tests and produces enhanced output with embedded trace hashes for cross-client differential testing.

### Installation

```bash
go build -o statetest-trace ./tests/fuzzers/statetest/cmd/statetest-trace/
```

### Quick Start

```bash
# Process a corpus directory
./statetest-trace process ./corpus ./enhanced_corpus

# Process with progress reporting and 16 workers
./statetest-trace process -w 16 -p ./corpus ./enhanced_corpus

# Dry run (validate without writing)
./statetest-trace process --dry-run ./corpus ./enhanced_corpus

# Verify an existing enhanced corpus
./statetest-trace verify ./enhanced_corpus

# Dump trace for a single test (debugging)
./statetest-trace dump ./test.json

# Show corpus statistics
./statetest-trace stats ./enhanced_corpus
```

### Subcommands

#### `process` - Process a corpus

Reads state tests from the input directory, executes them with trace normalization, and writes enhanced tests to the output directory.

```bash
statetest-trace process [flags] <input-dir> <output-dir>

Flags:
  -w, --workers int       Number of parallel workers (default: NumCPU)
  -t, --timeout duration  Timeout per test (default: 30s)
  -f, --fork string       Only process tests for specific fork
  -p, --progress          Show progress bar
      --skip-invalid      Skip invalid tests instead of erroring
  -q, --quiet             Suppress non-error output
  -v, --verbose           Show each processed file
      --dry-run           Validate without writing output
      --overwrite         Overwrite existing output files
```

#### `verify` - Verify trace hashes

Re-executes tests and compares computed trace hashes against stored values.

```bash
statetest-trace verify [flags] <corpus-dir>

Flags:
  -w, --workers int       Number of parallel workers
  -t, --timeout duration  Timeout per test (default: 30s)
  -v, --verbose           Show each verified file
  -q, --quiet             Suppress non-error output
```

#### `dump` - Dump normalized trace

Outputs the normalized trace for a single test in JSONL format.

```bash
statetest-trace dump [flags] <test-file>

Flags:
  -o, --output string     Output file path (default: stdout)
      --include-filtered  Include filtered entries (STOP, depth=0)
  -t, --timeout duration  Timeout (default: 30s)
```

#### `stats` - Show corpus statistics

Analyzes a corpus and reports statistics.

```bash
statetest-trace stats [flags] <corpus-dir>

Flags:
      --json   Output in JSON format
  -q, --quiet  Suppress output
```

### Output Format

The tool produces enhanced test files with embedded metadata:

```json
{
  "testName_d0g0v0": {
    "_info": {
      "comment": "Cross-VM consensus verification test",
      "generatedBy": "geth",
      "traceHash": "13251158d97ea2420d51501e32f818fc",
      "stateRoot": "0x60a3fe53c5486f7967c947766fce4e08...",
      "crossvmVersion": "1.0",
      "traceLines": 42,
      "generatedAt": "2025-12-08T10:00:00Z",
      "version": "dev"
    },
    "env": {...},
    "pre": {...},
    "transaction": {...},
    "post": {...}
  }
}
```

### Output Directory Structure

```
output_dir/
├── manifest.json           # Summary of processed tests
├── errors.log              # Any errors encountered
└── tests/
    ├── <tracehash1>.json
    ├── <tracehash2>.json
    └── ...
```

### Integration with Cross-Client Testing

1. **Generate enhanced corpus** with `statetest-trace process`
2. **Share corpus** with other client implementations
3. **Other clients verify** by re-executing and comparing trace hashes
4. **Detect divergences** when trace hashes don't match

See `STANDALONE_TRACE_TOOL_PLAN.md` for detailed design documentation.

## Credits

Based on the custom mutator fuzzer from [goevmlab](https://github.com/holiman/goevmlab) by Martin Holst Swende, with enhancements for coverage-guided prioritization.
