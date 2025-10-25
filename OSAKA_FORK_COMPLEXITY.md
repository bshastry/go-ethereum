# Osaka Fork: Additional Complexity Analysis

**Analysis Date:** 2025-10-25
**Codebase:** go-ethereum (geth)
**Fork:** Osaka (Part of Fusaka upgrade - Fulu + Osaka)

---

## Executive Summary

The Osaka fork introduces **5 major protocol changes** (EIPs) that add complexity to block processing and validation. Unlike Prague which introduced new protocol features (system contracts, requests), Osaka focuses on **security hardening, optimization, and infrastructure improvements**.

**Key Characteristics:**
- **Type:** Security + optimization fork
- **Blob schedule:** Same as Prague (Target: 6, Max: 9)
- **New precompile:** P256 signature verification (RIP-7212/EIP-7951)
- **Gas model changes:** ModExp precompile hardening
- **Resource limits:** Transaction gas cap, block size cap, blob count cap

---

## EIPs in Osaka Fork

| EIP | Name | Type | Impact Level |
|-----|------|------|--------------|
| **EIP-7212/7951** | P256 Verify Precompile | New Feature | HIGH |
| **EIP-7823** | ModExp Upper Bounds | Security | MEDIUM |
| **EIP-7883** | ModExp Gas Cost Increase | Gas Model | MEDIUM |
| **EIP-7825** | Max Transaction Gas Limit | Resource Limit | LOW |
| **EIP-7934** | Max Block Size | Resource Limit | LOW |

---

## 1. EIP-7212/7951: P256 Signature Verification Precompile

### Overview

Adds a new precompiled contract at address **0x0100** for secp256r1 (P-256) elliptic curve signature verification.

### Technical Details

**Address:** `0x0000000000000000000000000000000000000100`
**Gas Cost:** 3,450 gas (originally specified), 6,900 gas (EIP-7951 update)
**Input:** 160 bytes (hash + r + s + x + y)
**Output:** 32 bytes (0x00...01 if valid, 0x00...00 if invalid)

**Source Code:**
- Implementation: `/home/user/go-ethereum/core/vm/contracts.go:1426-1454`
- Added to `PrecompiledContractsOsaka` at line 171

### Why It's Needed

**Problem:** Ethereum natively supports secp256k1 (Bitcoin's curve) but not secp256r1 (P-256).

**Use Cases:**
- **Passkeys/WebAuthn:** Apple Secure Enclave, Android Keystore, Yubikey
- **Hardware wallets:** Most secure enclaves use P-256
- **Account abstraction:** Enables biometric authentication for Ethereum accounts
- **Cost reduction:** Without precompile, verification costs ~330,000 gas; with it: 3,450 gas (100x cheaper)

### Block Processing Impact

**Initialization Phase (Event Sequence Phase 3):**
```
When: VM context construction
Action: P256Verify precompile added to available precompiles map
Location: core/vm/contracts.go:218-219, 243-244
```

**Transaction Execution Phase (Event 30):**
```
When: Transaction calls address 0x0100
Action: P256 signature verification executed
Gas: 6,900 gas charged
Validation:
  - Input must be exactly 160 bytes
  - Point must be on P-256 curve
  - Signature must be valid ECDSA
```

### Consensus Impact

**High** - New cryptographic primitive that all nodes must implement identically.

**Risks:**
- Different P-256 implementations may have subtle bugs
- Point-at-infinity vulnerabilities (fixed in EIP-7951)
- Modular arithmetic edge cases
- Non-deterministic behavior across libraries

**Validation Requirements:**
- Test vectors MUST be identical across implementations
- Boundary cases: point at infinity, identity element
- Invalid curve points must be rejected
- Signature malleability must be consistent

---

## 2. EIP-7823: Set Upper Bounds for MODEXP

### Overview

Limits the maximum input size for the MODEXP precompile (address 0x05) to **1024 bytes per parameter**.

### Technical Details

**Precompile:** MODEXP (0x0000000000000000000000000000000000000005)
**Old Limit:** None (unlimited, subject to gas)
**New Limit:** 1024 bytes maximum for base, exponent, and modulus
**Rejection:** Transactions with larger inputs are rejected

**Source Code:**
```go
// core/vm/contracts.go:157
common.BytesToAddress([]byte{0x05}): &bigModExp{
    eip2565: true,
    eip7823: true,  // <-- Upper bounds enabled
    eip7883: true
}

// core/vm/contracts.go:626
if c.eip7823 && (inputLenOverflow || max(baseLen, expLen, modLen) > 1024) {
    return 0, errors.New("input too large")
}
```

### Why It's Needed

**Problem:** MODEXP has been a source of **numerous consensus bugs** due to:
- Impractical input lengths (e.g., 10,000+ byte modulus)
- Integer overflow in gas calculation
- Memory allocation attacks
- Different big integer library behaviors at extreme sizes

**Security Benefits:**
- Prevents DoS via extremely large modular exponentiation
- Reduces attack surface for consensus bugs
- Makes implementation testing tractable (2^10 bits vs unlimited)
- Enables formal verification of gas costs

### Block Processing Impact

**Transaction Validation Phase (Event 23f):**
```
When: MODEXP precompile called
Check: max(baseLen, expLen, modLen) <= 1024
Action: Reject if any parameter exceeds 1024 bytes
Error: "input too large"
```

**Gas Calculation:**
```
Before EIP-7823: Gas calculated for arbitrarily large inputs
After EIP-7823: Inputs > 1024 bytes rejected outright
```

### Consensus Impact

**Medium** - Changes acceptance criteria for transactions.

**Compatibility:**
- Transactions using MODEXP with >1024 byte inputs will fail on Osaka
- Unlikely to affect real-world usage (RSA-8192 = 1024 bytes exactly)
- May break hypothetical applications using very large numbers

---

## 3. EIP-7883: MODEXP Gas Cost Increase

### Overview

Increases gas costs for the MODEXP precompile to more accurately reflect computational complexity.

### Technical Details

**Precompile:** MODEXP (0x0000000000000000000000000000000000000005)

**Changes:**
1. **Minimum cost:** 200 gas → **500 gas** (+150%)
2. **General cost:** **3x increase** (tripled)
3. **Minimum parameter length:** Assume 32 bytes minimum for base/modulus
4. **Cost scaling:** More aggressive for parameters > 32 bytes

**Source Code:**
```go
// core/vm/contracts.go:600
if c.eip7883 {
    // Apply new gas calculation formula
    // Minimum 500 gas
    // Tripled base costs
    // Aggressive scaling for large inputs
}
```

### Why It's Needed

**Problem:** MODEXP was underpriced, enabling:
- Cheap DoS attacks (computation-heavy, low gas)
- Block stuffing with expensive operations
- Miner advantage (can skip validation of invalid blocks)

**Example:**
```
Before: MODEXP with 256-bit numbers = ~200 gas
After:  MODEXP with 256-bit numbers = ~600 gas (3x)

Before: MODEXP with 2048-bit RSA = ~2,000 gas
After:  MODEXP with 2048-bit RSA = ~6,000 gas (3x)
```

### Block Processing Impact

**Gas Calculation Phase (Event 24):**
```
When: Transaction calls MODEXP precompile
Change: Gas calculation uses new formula
Impact: Higher gas usage per call
```

**Block Gas Limit:**
```
Same 30M gas limit, but:
- Fewer MODEXP operations per block
- More accurate reflection of computation cost
```

### Consensus Impact

**Medium** - Changes gas economics but not execution results.

**Compatibility:**
- Transactions that were previously accepted may now run out of gas
- Gas estimation must be updated
- Applications using MODEXP need to increase gas limits

**Validation:**
- Gas calculation must be **exactly identical** across all implementations
- Different rounding could cause consensus divergence
- Test vectors critical for edge cases

---

## 4. EIP-7825: Maximum Transaction Gas Limit

### Overview

Caps individual transaction gas limit at **16,777,216 gas** (2^24).

### Technical Details

**Constant:** `params.MaxTxGas = 1 << 24 // 16,777,216`
**Block Gas Limit:** 30,000,000 (unchanged)
**Enforcement:** Pre-execution validation

**Source Code:**
```go
// params/protocol_params.go:31
MaxTxGas uint64 = 1 << 24 // 16,777,216

// core/state_transition.go:330-332
if isOsaka && msg.GasLimit > params.MaxTxGas {
    return fmt.Errorf("%w (cap: %d, tx: %d)",
        ErrGasLimitTooHigh, params.MaxTxGas, msg.GasLimit)
}
```

**Validation Points:**
1. Transaction pool: `core/txpool/validation.go:92-94`
2. State transition: `core/state_transition.go:330-332`
3. Miner filter: `miner/worker.go:482`
4. Gas estimator: `eth/gasestimator/gasestimator.go:67-78`
5. cmd/evm tool: `cmd/evm/internal/t8ntool/transaction.go:186-188`

### Why It's Needed

**Problem:** No upper limit on transaction gas allows:
- Transactions that consume 55% of block gas (16.7M / 30M)
- Complex DoS vectors
- Inefficient block packing
- Outlier transactions that break assumptions

**Benefits:**
- Guarantees at least ~1.79 transactions per block (30M / 16.7M)
- Simplifies gas estimation
- Reduces worst-case execution time
- Enables better block builder optimizations

### Block Processing Impact

**Pre-Transaction Validation (Event 23b):**
```
Event: Transaction validation
Check: msg.GasLimit <= params.MaxTxGas (16,777,216)
When: Before execution (preCheck phase)
Location: state_transition.go:330-332
Error: ErrGasLimitTooHigh if exceeded
```

**Transaction Pool:**
```
Transactions with gas > 16.7M:
- Rejected at transaction pool (before inclusion in block)
- Never propagated to network
- Invalid for mining
```

### Consensus Impact

**Low** - Validation rule, not execution change.

**Compatibility:**
- Hypothetical large transactions will be rejected
- Real-world impact minimal (99.9% of txs use < 10M gas)
- May affect contract deployment of very large contracts

---

## 5. EIP-7934: Maximum Block Size

### Overview

Caps the **RLP-encoded block size** at **8,388,608 bytes** (8 MiB).

### Technical Details

**Constant:** `params.MaxBlockSize = 8_388_608 // 8 MiB`
**Encoding:** RLP (Recursive Length Prefix)
**Enforcement:** Block validation (post-execution)

**Source Code:**
```go
// params/protocol_params.go:185
MaxBlockSize = 8_388_608 // 8 MiB

// core/block_validator.go:53-55
if v.config.IsOsaka(block.Number(), block.Time()) &&
   block.Size() > params.MaxBlockSize {
    return ErrBlockOversized
}
```

### Why It's Needed

**Problem:** Without blob transactions, calldata was unlimited (subject to gas). With blobs:
- Blob data is not in block (external)
- Calldata can still bloat blocks
- Network propagation delays
- State sync issues

**Block Size Components:**
```
Block (RLP-encoded):
├─ Header                (~500 bytes)
├─ Transactions          (variable)
│  ├─ Signatures        (65 bytes each)
│  ├─ Calldata          (gas-limited)
│  └─ Metadata          (~100 bytes each)
├─ Uncles                (~500 bytes each, max 2)
└─ Withdrawals           (Shanghai+, ~75 bytes each)

Max theoretical before EIP-7934: ~7.2 MB with 30M gas
Max with EIP-7934: 8 MiB hard cap
```

### Block Processing Impact

**Block Validation Phase (Event 64):**
```
Phase: ValidateBody (before execution)
Check: block.Size() <= params.MaxBlockSize
When: After receiving block, before processing
Location: block_validator.go:53-55
Error: ErrBlockOversized if exceeded
```

**Miner Optimization:**
```go
// miner/worker.go:68-117
maxBlockSize := params.MaxBlockSize - maxBlockSizeBufferZone

// Leave 512 KB buffer to account for:
// - Header size changes
// - Signature variations
// - Encoding overhead
```

### Consensus Impact

**Low** - Rarely triggered in practice.

**Compatibility:**
- Blocks exceeding 8 MiB rejected
- Unlikely scenario (requires filling block with calldata)
- Provides deterministic upper bound for network layer

---

## Blob Configuration (Unchanged from Prague)

**Configuration:**
```go
DefaultOsakaBlobConfig = &BlobConfig{
    Target:         6,  // Same as Prague
    Max:            9,  // Same as Prague
    UpdateFraction: 5007716,  // Same as Prague
}
```

**Comparison:**

| Fork | Target Blobs | Max Blobs | Target Blob Gas | Max Blob Gas |
|------|--------------|-----------|-----------------|--------------|
| Cancun | 3 | 6 | 393,216 | 786,432 |
| Prague | 6 | 9 | 786,432 | 1,179,648 |
| **Osaka** | **6** | **9** | **786,432** | **1,179,648** |

**Blob Transaction Limits (New):**
- **Before Osaka:** No per-transaction blob limit
- **Osaka:** Maximum **6 blobs per transaction** (EIP-4844 constant)

**Enforcement:**
```go
// core/state_transition.go:376-378
if isOsaka && len(msg.BlobHashes) > params.BlobTxMaxBlobs {
    return ErrTooManyBlobs
}

// params.BlobTxMaxBlobs = 6
```

---

## Summary of Block Processing Changes

### Initialization Phase

**Added:**
1. P256Verify precompile to precompile map (address 0x0100)
2. MODEXP updated with eip7823=true, eip7883=true flags

### Transaction Validation Phase

**New Checks:**
```
23b. Check: tx.GasLimit <= 16,777,216 (EIP-7825)
     Location: state_transition.go:330
     Error: ErrGasLimitTooHigh

23e. Check: len(blobHashes) <= 6 (blob limit)
     Location: state_transition.go:376
     Error: ErrTooManyBlobs
```

### Precompile Execution Phase

**Changed:**
```
MODEXP (0x05):
  - Input validation: Reject if any param > 1024 bytes (EIP-7823)
  - Gas calculation: New formula (EIP-7883)
    * Minimum: 500 gas (was 200)
    * Base cost: 3x increase
    * Scaling: More aggressive for large inputs

P256Verify (0x0100): NEW
  - Input: 160 bytes (hash + signature + pubkey)
  - Gas: 6,900 gas
  - Validation: Point on curve, valid ECDSA signature
```

### Block Validation Phase

**Added:**
```
64. Check: block.Size() <= 8,388,608 bytes (EIP-7934)
    Location: block_validator.go:53
    Error: ErrBlockOversized
```

---

## Complexity Analysis

### Implementation Complexity

| EIP | Complexity | Reason |
|-----|------------|--------|
| EIP-7951 | **HIGH** | New cryptographic primitive, curve arithmetic |
| EIP-7823 | **LOW** | Simple bounds check |
| EIP-7883 | **MEDIUM** | Complex gas formula changes |
| EIP-7825 | **LOW** | Simple constant check |
| EIP-7934 | **LOW** | Block size check |

### Testing Burden

| EIP | Test Vectors Required | Edge Cases |
|-----|----------------------|------------|
| EIP-7951 | **1000+** | Point at infinity, invalid points, malleability |
| EIP-7823 | **20** | Exactly 1024, 1025, overflow cases |
| EIP-7883 | **100** | Gas calculation boundary cases |
| EIP-7825 | **5** | Exactly 2^24, 2^24+1 |
| EIP-7934 | **5** | Exactly 8 MiB, 8 MiB + 1 |

### Consensus Risk

**High Risk:**
- **EIP-7951 (P256Verify):** Cryptographic implementation differences
- **EIP-7883 (MODEXP gas):** Gas calculation precision differences

**Medium Risk:**
- **EIP-7823 (MODEXP bounds):** Integer overflow in length calculation

**Low Risk:**
- **EIP-7825:** Simple constant comparison
- **EIP-7934:** Simple block size check

---

## Comparison with Prague Fork

| Aspect | Prague | Osaka |
|--------|--------|-------|
| **Type** | Feature fork | Optimization + security fork |
| **System contracts** | 4 new (beacon, history, withdrawal, consolidation) | 0 new |
| **Execution requests** | 3 types (deposits, withdrawals, consolidations) | Unchanged |
| **Precompiles** | 0 new | 1 new (P256) |
| **Gas model changes** | 0 | 2 (MODEXP) |
| **Resource limits** | 0 | 3 (tx gas, block size, blob count) |
| **Blob schedule** | Doubled (3→6 target, 6→9 max) | Unchanged from Prague |
| **Complexity** | High (new protocol features) | Medium (hardening + optimization) |

---

## Recommendations for Implementation

### 1. P256 Signature Verification

**Critical:**
- Use well-tested P-256 library (e.g., OpenSSL, secp256r1-rs)
- Implement point validation (reject invalid curve points)
- Test against EIP-7951 vectors (fixed point-at-infinity bug)
- Ensure deterministic behavior across platforms

**Test Cases:**
- Valid signatures
- Invalid signatures
- Point at infinity
- Points not on curve
- Malformed inputs (wrong length)
- Zero signature components

### 2. MODEXP Changes

**EIP-7823 (Bounds):**
```rust
fn validate_modexp_input(base_len, exp_len, mod_len) -> Result<()> {
    if base_len > 1024 || exp_len > 1024 || mod_len > 1024 {
        return Err("input too large");
    }
    Ok(())
}
```

**EIP-7883 (Gas):**
- Use reference implementation's gas formula exactly
- Test against all reference test vectors
- Pay special attention to rounding/precision

### 3. Transaction Validation

**EIP-7825:**
```rust
if is_osaka && tx.gas_limit > 16_777_216 {
    return Err(Error::GasLimitTooHigh);
}
```

**Blob Count:**
```rust
if is_osaka && tx.blob_hashes.len() > 6 {
    return Err(Error::TooManyBlobs);
}
```

### 4. Block Validation

**EIP-7934:**
```rust
if is_osaka && block.rlp_size() > 8_388_608 {
    return Err(Error::BlockOversized);
}
```

---

## Impact on Block Processing Trace

### New Events

**Event 23g: EIP-7825 Gas Limit Check**
```
Location: state_transition.go:330
Check: tx.gasLimit <= MaxTxGas (16,777,216)
Action: Reject transaction if exceeded
```

**Event 23h: Blob Count Check**
```
Location: state_transition.go:376
Check: len(blobHashes) <= BlobTxMaxBlobs (6)
Action: Reject transaction if exceeded
```

**Event 30c: P256Verify Precompile Execution**
```
Location: vm/contracts.go:1434
When: CALL to 0x0100
Gas: 6,900
Action: Verify P-256 ECDSA signature
```

**Event 64: Block Size Validation**
```
Location: block_validator.go:53
Check: block.Size() <= MaxBlockSize (8,388,608)
Action: Reject block if exceeded
```

### Modified Events

**Event 24 (Modified): Intrinsic Gas Calculation**
```
Change: MODEXP precompile gas calculation
Impact: Higher gas for MODEXP calls
Formula: EIP-7883 new formula (3x base cost)
```

**Event 30 (Modified): Precompile Execution**
```
MODEXP (0x05):
  - New: Input size validation (EIP-7823)
  - New: Gas calculation (EIP-7883)

P256Verify (0x0100):
  - New: Entire precompile
```

---

## Conclusion

The Osaka fork adds **moderate complexity** focused on:

1. **Security hardening** (MODEXP bounds, block size limit)
2. **Account abstraction infrastructure** (P256 precompile)
3. **Gas model accuracy** (MODEXP repricing)
4. **Resource limits** (tx gas cap, blob count)

**Key Takeaway:** Unlike Prague's feature additions, Osaka is a **stabilization and optimization** fork that addresses technical debt and security concerns while enabling better user experience through hardware wallet support.

**Testing Priority:**
1. **Highest:** P256 signature verification (consensus-critical cryptography)
2. **High:** MODEXP gas calculation (consensus-critical economics)
3. **Medium:** MODEXP bounds, tx gas limit, blob count
4. **Low:** Block size limit (rarely triggered)

**Estimated Additional Trace Events:** +4 validation checks, +1 precompile, modified gas calculations

**Overall Complexity Rating:** 6/10 (compared to Prague: 8/10, Cancun: 7/10)
