// Copyright 2015 The go-ethereum Authors
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

package core

import (
	"crypto/sha256"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/misc"
	"github.com/ethereum/go-ethereum/consensus/misc/eip4844"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	chain ChainContext // Chain context interface
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(chain ChainContext) *StateProcessor {
	return &StateProcessor{
		chain: chain,
	}
}

// chainConfig returns the chain configuration.
func (p *StateProcessor) chainConfig() *params.ChainConfig {
	return p.chain.Config()
}

// Process processes the state changes according to the Ethereum rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (*ProcessResult, error) {
	var (
		config      = p.chainConfig()
		receipts    types.Receipts
		usedGas     = new(uint64)
		header      = block.Header()
		blockHash   = block.Hash()
		blockNumber = block.Number()
		allLogs     []*types.Log
		gp          = new(GasPool).AddGas(block.GasLimit())
	)

	// Mutate the block and state according to any hard-fork specs
	if config.DAOForkSupport && config.DAOForkBlock != nil && config.DAOForkBlock.Cmp(block.Number()) == 0 {
		misc.ApplyDAOHardFork(statedb)
	}
	var (
		context vm.BlockContext
		signer  = types.MakeSigner(config, header.Number, header.Time)
	)

	// Apply pre-execution system calls.
	var tracingStateDB = vm.StateDB(statedb)
	if hooks := cfg.Tracer; hooks != nil {
		tracingStateDB = state.NewHookedState(statedb, hooks)
	}
	context = NewEVMBlockContext(header, p.chain, nil)
	evm := vm.NewEVM(context, tracingStateDB, config, cfg)

	if beaconRoot := block.BeaconRoot(); beaconRoot != nil {
		ProcessBeaconBlockRoot(*beaconRoot, evm)
	}
	if config.IsPrague(block.Number(), block.Time()) || config.IsVerkle(block.Number(), block.Time()) {
		ProcessParentBlockHash(block.ParentHash(), evm)
	}

	// Iterate over and process the individual transactions
	// Collect per-transaction gas data for validation tracing (stored in ProcessResult)
	var txGasInfos []TxGasInfo

	for i, tx := range block.Transactions() {
		msg, err := TransactionToMessage(tx, signer, header.BaseFee)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		statedb.SetTxContext(tx.Hash(), i)

		receipt, err := ApplyTransactionWithEVM(msg, gp, statedb, blockNumber, blockHash, context.Time, tx, usedGas, evm)
		if err != nil {
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx.Hash().Hex(), err)
		}
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)

		// Collect per-transaction gas info for trace validation
		txGasInfos = append(txGasInfos, TxGasInfo{
			TxIndex:           toHex(uint64(i)),
			GasLimit:          toHex(tx.Gas()),
			GasUsed:           toHex(receipt.GasUsed),
			CumulativeGasUsed: toHex(receipt.CumulativeGasUsed),
		})
	}

	// Block-level trace: Withdrawals (EIP-4895) - process before execution requests
	// This matches Nethermind's trace ordering
	if len(block.Body().Withdrawals) > 0 {
		if hooks := cfg.Tracer; hooks != nil && hooks.OnPostExecutionStart != nil {
			hooks.OnPostExecutionStart("withdrawals", "4895")
		}

		// Track account lifecycle for withdrawals
		var accountsCreated uint64
		var emptyAccountsDeleted uint64

		// Process withdrawals and track balance changes
		var totalWithdrawn uint64
		withdrawalData := make([]map[string]interface{}, 0, len(block.Body().Withdrawals))

		for _, w := range block.Body().Withdrawals {
			// Get balance before withdrawal
			balanceBefore := statedb.GetBalance(w.Address)

			// Check if account exists before withdrawal
			accountExistedBefore := statedb.Exist(w.Address)

			// Convert amount from gwei to wei
			amount := new(uint256.Int).SetUint64(w.Amount)
			amountWei := new(uint256.Int).Mul(amount, uint256.NewInt(params.GWei))

			// Apply withdrawal
			statedb.AddBalance(w.Address, amountWei, tracing.BalanceIncreaseWithdrawal)

			// Get balance after withdrawal
			balanceAfter := statedb.GetBalance(w.Address)

			// Check if account was created by this withdrawal
			if !accountExistedBefore && statedb.Exist(w.Address) {
				accountsCreated++
			}

			// Track total withdrawn (in gwei)
			totalWithdrawn += w.Amount

			// Build withdrawal trace entry with balance tracking
			// Use lowercase address as per canonical format (not EIP-55 checksum)
			withdrawalEntry := map[string]interface{}{
				"address":        strings.ToLower(w.Address.Hex()),
				"amountGwei":     toHex(w.Amount),
				"index":          toHex(w.Index),
				"validatorIndex": toHex(w.Validator),
			}

			// Add balance change tracking
			balanceDelta := new(uint256.Int).Sub(balanceAfter, balanceBefore)
			withdrawalEntry["balanceChange"] = map[string]interface{}{
				"before": balanceBefore.Hex(),
				"after":  balanceAfter.Hex(),
				"delta":  balanceDelta.Hex(),
			}

			withdrawalData = append(withdrawalData, withdrawalEntry)
		}

		// Emit withdrawal trace with canonical format
		if hooks := cfg.Tracer; hooks != nil && hooks.OnPostExecutionEnd != nil {
			hooks.OnPostExecutionEnd(map[string]interface{}{
				"accountsCreated":      toHex(accountsCreated),
				"emptyAccountsDeleted": toHex(emptyAccountsDeleted),
				"totalWithdrawn":       toHex(totalWithdrawn),
				"withdrawals":          withdrawalData,
			})
		}
	}

	// Read requests if Prague is enabled.
	var requests [][]byte
	if config.IsPrague(block.Number(), block.Time()) {
		// Block-level trace: Post-execution start for execution requests (EIP-7685)
		if hooks := cfg.Tracer; hooks != nil && hooks.OnPostExecutionStart != nil {
			hooks.OnPostExecutionStart("executionRequests", "7685")
		}

		requests = [][]byte{}
		// EIP-6110
		if err := ParseDepositLogs(&requests, allLogs, config); err != nil {
			return nil, fmt.Errorf("failed to parse deposit logs: %w", err)
		}
		// EIP-7002
		if err := ProcessWithdrawalQueue(&requests, evm); err != nil {
			return nil, fmt.Errorf("failed to process withdrawal queue: %w", err)
		}
		// EIP-7251
		if err := ProcessConsolidationQueue(&requests, evm); err != nil {
			return nil, fmt.Errorf("failed to process consolidation queue: %w", err)
		}

		// Block-level trace: Post-execution end with request details
		if hooks := cfg.Tracer; hooks != nil && hooks.OnPostExecutionEnd != nil {
			// Build requests array according to spec
			requestsArray := make([]map[string]interface{}, 0, len(requests))
			for _, req := range requests {
				if len(req) > 0 {
					var requestName string
					var eipNum string
					switch req[0] {
					case 0x00:
						requestName = "deposit"
						eipNum = "6110"
					case 0x01:
						requestName = "withdrawal"
						eipNum = "7002"
					case 0x02:
						requestName = "consolidation"
						eipNum = "7251"
					default:
						requestName = "unknown"
						eipNum = "unknown"
					}
					requestsArray = append(requestsArray, map[string]interface{}{
						"requestType": fmt.Sprintf("0x%02x", req[0]),
						"requestName": requestName,
						"eip":         eipNum,
						"rawBytes":    common.Bytes2Hex(req),
					})
				}
			}

			// Compute requestsHash (SHA256 of concatenated requests)
			var requestsHash string
			if len(requests) == 0 {
				// Empty SHA256 hash
				requestsHash = "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
			} else {
				// Concatenate all requests and hash
				var concatenated []byte
				for _, req := range requests {
					concatenated = append(concatenated, req...)
				}
				hash := sha256.Sum256(concatenated)
				requestsHash = "0x" + common.Bytes2Hex(hash[:])
			}

			hooks.OnPostExecutionEnd(map[string]interface{}{
				"hashCalculation": map[string]interface{}{
					"hashMethod":     "sha256",
					"sortedRequests": true,
				},
				"requests":     requestsArray,
				"requestsHash": requestsHash,
			})
		}
	}

	// NOTE: Validation traces (gasAccounting, blobGasAccounting) have been moved to
	// EmitValidationTraces() which is called from blockchain.go AFTER ValidateState() succeeds.
	// This ensures we only emit validation traces for blocks that pass all validation checks.

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	// Note: Withdrawals have already been processed above (before execution requests) to match
	// Nethermind's trace ordering. We pass an empty withdrawal list to Finalize to prevent
	// double-processing while still allowing other finalization logic (e.g. pre-merge rewards).
	bodyWithoutWithdrawals := &types.Body{
		Transactions: block.Body().Transactions,
		Uncles:       block.Body().Uncles,
		Withdrawals:  nil, // Already processed above
	}
	p.chain.Engine().Finalize(p.chain, header, tracingStateDB, bodyWithoutWithdrawals)

	return &ProcessResult{
		Receipts:   receipts,
		Requests:   requests,
		Logs:       allLogs,
		GasUsed:    *usedGas,
		TxGasInfos: txGasInfos,
	}, nil
}

// EmitValidationTraces emits block-level validation traces after successful state validation.
// This must be called AFTER ValidateState() to ensure we only trace valid blocks.
func EmitValidationTraces(block *types.Block, res *ProcessResult, config *params.ChainConfig, hooks *tracing.Hooks) {
	if hooks == nil || hooks.OnValidation == nil {
		return
	}

	header := block.Header()

	// Block-level trace: Validation - gas accounting
	gasLimitExceeded := res.GasUsed > header.GasLimit
	overallResult := "valid"
	if gasLimitExceeded {
		overallResult = "invalid"
	}
	hooks.OnValidation("gasAccounting", map[string]interface{}{
		"blockGasLimit":    toHex(header.GasLimit),
		"gasLimitExceeded": gasLimitExceeded,
		"overallResult":    overallResult,
		"totalGasUsed":     toHex(res.GasUsed),
		"transactions":     res.TxGasInfos,
		"valid":            !gasLimitExceeded,
	})

	// Block-level trace: Validation - blob gas accounting (if post-Cancun)
	if config.IsCancun(block.Number(), block.Time()) {
		var totalBlobGasUsed uint64
		blobTxInfos := make([]map[string]interface{}, 0)

		// Collect per-transaction blob gas info
		for i, receipt := range res.Receipts {
			if receipt.BlobGasUsed > 0 {
				totalBlobGasUsed += receipt.BlobGasUsed
				blobCount := receipt.BlobGasUsed / params.BlobTxBlobGasPerBlob
				blobTxInfos = append(blobTxInfos, map[string]interface{}{
					"blobCount":             toHex(blobCount),
					"blobGasPerBlob":        toHex(params.BlobTxBlobGasPerBlob),
					"blobGasUsed":           toHex(receipt.BlobGasUsed),
					"cumulativeBlobGasUsed": toHex(totalBlobGasUsed),
					"txIndex":               toHex(uint64(i)),
				})
			}
		}

		maxBlobGas := eip4844.MaxBlobGasPerBlock(config, block.Time())
		blobGasLimitExceeded := totalBlobGasUsed > maxBlobGas

		// Calculate blob gas price using fake exponential
		excessBlobGas := *header.ExcessBlobGas
		blobGasPrice := eip4844.CalcBlobFee(config, header)

		// Trace the fake exponential calculation for transparency
		// factor = 1 Wei (minBlobGasPrice), numerator = excessBlobGas, denominator = UPDATE_FRACTION
		// Get the fork-specific UpdateFraction for accurate tracing
		blobConfig := eip4844.LatestBlobConfig(config, block.Time())
		minPrice := big.NewInt(params.BlobTxMinBlobGasprice)
		denominator := new(big.Int).SetUint64(blobConfig.UpdateFraction)
		numerator := new(big.Int).SetUint64(excessBlobGas)

		// Build fake exponential trace with first iteration
		iterations := []map[string]interface{}{
			{
				"accumulator": toHexBig(new(big.Int).Mul(minPrice, denominator)),
				"i":           "0x0",
				"overflow":    false,
			},
		}

		overallResult := "valid"
		if blobGasLimitExceeded {
			overallResult = "invalid"
		}

		hooks.OnValidation("blobGasAccounting", map[string]interface{}{
			"blobGasLimitExceeded": blobGasLimitExceeded,
			"blobGasPriceCalculation": map[string]interface{}{
				"blobGasPrice":  toHexBig(blobGasPrice),
				"excessBlobGas": toHex(excessBlobGas),
				"fakeExponential": map[string]interface{}{
					"denominator": toHexBig(denominator),
					"factor":      toHexBig(minPrice),
					"iterations":  iterations,
					"numerator":   toHexBig(numerator),
					"result":      toHexBig(blobGasPrice),
				},
				"minBlobGasPrice": toHexBig(minPrice),
			},
			"maxBlobGasPerBlock": toHex(maxBlobGas),
			"overallResult":      overallResult,
			"totalBlobGasUsed":   toHex(totalBlobGasUsed),
			"transactions":       blobTxInfos,
			"valid":              !blobGasLimitExceeded,
		})
	}
}

// toHex converts a uint64 to a 0x-prefixed hexadecimal string.
func toHex(n uint64) string {
	return fmt.Sprintf("0x%x", n)
}

// toHexBig converts a big.Int to a 0x-prefixed hexadecimal string.
// Returns "0x0" for nil values.
func toHexBig(n *big.Int) string {
	if n == nil {
		return "0x0"
	}
	if n.Sign() == 0 {
		return "0x0"
	}
	return "0x" + n.Text(16)
}

// ApplyTransactionWithEVM attempts to apply a transaction to the given state database
// and uses the input parameters for its environment similar to ApplyTransaction. However,
// this method takes an already created EVM instance as input.
func ApplyTransactionWithEVM(msg *Message, gp *GasPool, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, usedGas *uint64, evm *vm.EVM) (receipt *types.Receipt, err error) {
	if hooks := evm.Config.Tracer; hooks != nil {
		if hooks.OnTxStart != nil {
			hooks.OnTxStart(evm.GetVMContext(), tx, msg.From)
		}
		if hooks.OnTxEnd != nil {
			defer func() { hooks.OnTxEnd(receipt, err) }()
		}
	}
	// Apply the transaction to the current state (included in the env).
	result, err := ApplyMessage(evm, msg, gp)
	if err != nil {
		return nil, err
	}
	// Update the state with pending changes.
	var root []byte
	if evm.ChainConfig().IsByzantium(blockNumber) {
		evm.StateDB.Finalise(true)
	} else {
		root = statedb.IntermediateRoot(evm.ChainConfig().IsEIP158(blockNumber)).Bytes()
	}
	*usedGas += result.UsedGas

	// Merge the tx-local access event into the "block-local" one, in order to collect
	// all values, so that the witness can be built.
	if statedb.Database().TrieDB().IsVerkle() {
		statedb.AccessEvents().Merge(evm.AccessEvents)
	}
	return MakeReceipt(evm, result, statedb, blockNumber, blockHash, blockTime, tx, *usedGas, root), nil
}

// MakeReceipt generates the receipt object for a transaction given its execution result.
func MakeReceipt(evm *vm.EVM, result *ExecutionResult, statedb *state.StateDB, blockNumber *big.Int, blockHash common.Hash, blockTime uint64, tx *types.Transaction, usedGas uint64, root []byte) *types.Receipt {
	// Create a new receipt for the transaction, storing the intermediate root and gas used
	// by the tx.
	receipt := &types.Receipt{Type: tx.Type(), PostState: root, CumulativeGasUsed: usedGas}
	if result.Failed() {
		receipt.Status = types.ReceiptStatusFailed
	} else {
		receipt.Status = types.ReceiptStatusSuccessful
	}
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas

	if tx.Type() == types.BlobTxType {
		receipt.BlobGasUsed = uint64(len(tx.BlobHashes()) * params.BlobTxBlobGasPerBlob)
		receipt.BlobGasPrice = evm.Context.BlobBaseFee
	}

	// If the transaction created a contract, store the creation address in the receipt.
	if tx.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(evm.TxContext.Origin, tx.Nonce())
	}

	// Set the receipt logs and create the bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash(), blockNumber.Uint64(), blockHash, blockTime)
	receipt.Bloom = types.CreateBloom(receipt)
	receipt.BlockHash = blockHash
	receipt.BlockNumber = blockNumber
	receipt.TransactionIndex = uint(statedb.TxIndex())
	return receipt
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(evm *vm.EVM, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64) (*types.Receipt, error) {
	msg, err := TransactionToMessage(tx, types.MakeSigner(evm.ChainConfig(), header.Number, header.Time), header.BaseFee)
	if err != nil {
		return nil, err
	}
	// Create a new context to be used in the EVM environment
	return ApplyTransactionWithEVM(msg, gp, statedb, header.Number, header.Hash(), header.Time, tx, usedGas, evm)
}

// ProcessBeaconBlockRoot applies the EIP-4788 system call to the beacon block root
// contract. This method is exported to be used in tests.
func ProcessBeaconBlockRoot(beaconRoot common.Hash, evm *vm.EVM) {
	// Calculate ring buffer parameters per EIP-4788 specification:
	// - timestamp is stored at: timestamp % HISTORY_BUFFER_LENGTH
	// - beacon root is stored at: (timestamp % HISTORY_BUFFER_LENGTH) + HISTORY_BUFFER_LENGTH
	const historyBufferLength = 8191
	ringBufferIndex := evm.Context.Time % historyBufferLength
	timestampSlot := ringBufferIndex
	rootSlot := ringBufferIndex + historyBufferLength

	// Capture old storage values before the call
	var oldTimestampValue, oldRootValue common.Hash
	if tracer := evm.Config.Tracer; tracer != nil && tracer.OnPreExecutionStart != nil {
		oldTimestampValue = evm.StateDB.GetState(params.BeaconRootsAddress,
			common.BigToHash(new(big.Int).SetUint64(timestampSlot)))
		oldRootValue = evm.StateDB.GetState(params.BeaconRootsAddress,
			common.BigToHash(new(big.Int).SetUint64(rootSlot)))

		// Block-level trace: Pre-execution start for EIP-4788 with ring buffer metadata
		metadata := map[string]interface{}{
			"timestamp":             toHex(evm.Context.Time),
			"parentBeaconBlockRoot": beaconRoot.Hex(),
			"contractAddress":       params.BeaconRootsAddress.Hex(),
			"ringBuffer": map[string]interface{}{
				"index":         toHex(ringBufferIndex),
				"timestampSlot": toHex(timestampSlot),
				"rootSlot":      toHex(rootSlot),
			},
			// Include old values for tracer to use if needed (internal tracking fields)
			"_oldTimestamp": oldTimestampValue.Hex(),
			"_oldRoot":      oldRootValue.Hex(),
		}

		tracer.OnPreExecutionStart("beaconRootStorage", "4788", metadata)
	}

	// Transaction-level trace
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}

	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.BeaconRootsAddress,
		Data:      beaconRoot[:],
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(params.BeaconRootsAddress)

	// Track gas before the call to measure actual consumption
	gasStart := uint64(30_000_000)
	_, gasRemaining, _ := evm.Call(msg.From, *msg.To, msg.Data, gasStart, common.U2560)
	actualGasUsed := gasStart - gasRemaining
	evm.StateDB.Finalise(true)

	// Block-level trace: Pre-execution end with actual gas used and storage writes
	if tracer := evm.Config.Tracer; tracer != nil && tracer.OnPreExecutionEnd != nil {
		// Capture new storage values after the call
		newTimestampValue := evm.StateDB.GetState(params.BeaconRootsAddress,
			common.BigToHash(new(big.Int).SetUint64(timestampSlot)))
		newRootValue := evm.StateDB.GetState(params.BeaconRootsAddress,
			common.BigToHash(new(big.Int).SetUint64(rootSlot)))

		// Build storage writes metadata
		storageWrites := []map[string]interface{}{
			{
				"slot":     toHex(timestampSlot),
				"oldValue": oldTimestampValue.Hex(),
				"newValue": newTimestampValue.Hex(),
			},
			{
				"slot":     toHex(rootSlot),
				"oldValue": oldRootValue.Hex(),
				"newValue": newRootValue.Hex(),
			},
		}

		metadata := map[string]interface{}{
			"storageWrites": storageWrites,
		}

		// Calculate intrinsic gas using the standard IntrinsicGas function
		// which properly accounts for zero vs non-zero bytes in calldata.
		// This matches how Nethermind and other clients calculate system call gas.
		accessList := types.AccessList{{Address: params.BeaconRootsAddress}}
		intrinsicGas, _ := IntrinsicGas(msg.Data, accessList, nil, false, true, true, false)

		// Report total gas (execution + intrinsic) to match other clients like Nethermind
		totalGasUsed := actualGasUsed + intrinsicGas

		tracer.OnPreExecutionEnd(totalGasUsed, metadata)
	}
}

// ProcessParentBlockHash stores the parent block hash in the history storage contract
// as per EIP-2935/7709.
func ProcessParentBlockHash(prevHash common.Hash, evm *vm.EVM) {
	// Calculate ring buffer parameters per EIP-2935 specification:
	// - Storage slot is: (blockNumber - 1) % HISTORY_SERVE_WINDOW
	// - For EIP-2935, slot == index (single slot per block)
	const historyServeWindow = 8191
	ringBufferIndex := (evm.Context.BlockNumber.Uint64() - 1) % historyServeWindow
	storageSlot := ringBufferIndex

	// Capture old storage value before the call
	var oldParentHashValue common.Hash
	if tracer := evm.Config.Tracer; tracer != nil && tracer.OnPreExecutionStart != nil {
		oldParentHashValue = evm.StateDB.GetState(params.HistoryStorageAddress,
			common.BigToHash(new(big.Int).SetUint64(storageSlot)))

		// Block-level trace: Pre-execution start for EIP-2935 with ring buffer metadata
		metadata := map[string]interface{}{
			"blockNumber":     toHex(evm.Context.BlockNumber.Uint64()),
			"parentHash":      prevHash.Hex(),
			"contractAddress": params.HistoryStorageAddress.Hex(),
			"ringBuffer": map[string]interface{}{
				"index": toHex(ringBufferIndex),
				"slot":  toHex(storageSlot),
			},
			// Include old value for tracer to use if needed (internal tracking field)
			"_oldParentHash": oldParentHashValue.Hex(),
		}

		tracer.OnPreExecutionStart("blockHashStorage", "2935", metadata)
	}

	if tracer := evm.Config.Tracer; tracer != nil {
		// Transaction-level trace
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &params.HistoryStorageAddress,
		Data:      prevHash.Bytes(),
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(params.HistoryStorageAddress)

	// Track gas before the call to measure actual consumption
	gasStart := uint64(30_000_000)
	_, gasRemaining, err := evm.Call(msg.From, *msg.To, msg.Data, gasStart, common.U2560)
	if err != nil {
		panic(err)
	}
	actualGasUsed := gasStart - gasRemaining

	if evm.StateDB.AccessEvents() != nil {
		evm.StateDB.AccessEvents().Merge(evm.AccessEvents)
	}
	evm.StateDB.Finalise(true)

	// Block-level trace: Pre-execution end with actual gas used and storage writes
	if tracer := evm.Config.Tracer; tracer != nil && tracer.OnPreExecutionEnd != nil {
		// Capture new storage value after the call
		newParentHashValue := evm.StateDB.GetState(params.HistoryStorageAddress,
			common.BigToHash(new(big.Int).SetUint64(storageSlot)))

		// Build storage writes metadata
		storageWrites := []map[string]interface{}{
			{
				"slot":     toHex(storageSlot),
				"oldValue": oldParentHashValue.Hex(),
				"newValue": newParentHashValue.Hex(),
			},
		}

		metadata := map[string]interface{}{
			"storageWrites": storageWrites,
		}

		// Calculate intrinsic gas using the standard IntrinsicGas function
		// which properly accounts for zero vs non-zero bytes in calldata.
		// This matches how Nethermind and other clients calculate system call gas.
		intrinsicGas, _ := IntrinsicGas(msg.Data, nil, nil, false, true, true, false)

		// Report total gas (execution + intrinsic) to match other clients like Nethermind
		totalGasUsed := actualGasUsed + intrinsicGas

		tracer.OnPreExecutionEnd(totalGasUsed, metadata)
	}
}

// ProcessWithdrawalQueue calls the EIP-7002 withdrawal queue contract.
// It returns the opaque request data returned by the contract.
func ProcessWithdrawalQueue(requests *[][]byte, evm *vm.EVM) error {
	return processRequestsSystemCall(requests, evm, 0x01, params.WithdrawalQueueAddress)
}

// ProcessConsolidationQueue calls the EIP-7251 consolidation queue contract.
// It returns the opaque request data returned by the contract.
func ProcessConsolidationQueue(requests *[][]byte, evm *vm.EVM) error {
	return processRequestsSystemCall(requests, evm, 0x02, params.ConsolidationQueueAddress)
}

func processRequestsSystemCall(requests *[][]byte, evm *vm.EVM, requestType byte, addr common.Address) error {
	if tracer := evm.Config.Tracer; tracer != nil {
		onSystemCallStart(tracer, evm.GetVMContext())
		if tracer.OnSystemCallEnd != nil {
			defer tracer.OnSystemCallEnd()
		}
	}
	msg := &Message{
		From:      params.SystemAddress,
		GasLimit:  30_000_000,
		GasPrice:  common.Big0,
		GasFeeCap: common.Big0,
		GasTipCap: common.Big0,
		To:        &addr,
	}
	evm.SetTxContext(NewEVMTxContext(msg))
	evm.StateDB.AddAddressToAccessList(addr)
	ret, _, err := evm.Call(msg.From, *msg.To, msg.Data, 30_000_000, common.U2560)
	evm.StateDB.Finalise(true)
	if err != nil {
		return fmt.Errorf("system call failed to execute: %v", err)
	}
	if len(ret) == 0 {
		return nil // skip empty output
	}
	// Append prefixed requestsData to the requests list.
	requestsData := make([]byte, len(ret)+1)
	requestsData[0] = requestType
	copy(requestsData[1:], ret)
	*requests = append(*requests, requestsData)
	return nil
}

var depositTopic = common.HexToHash("0x649bbc62d0e31342afea4e5cd82d4049e7e1ee912fc0889aa790803be39038c5")

// ParseDepositLogs extracts the EIP-6110 deposit values from logs emitted by
// BeaconDepositContract.
func ParseDepositLogs(requests *[][]byte, logs []*types.Log, config *params.ChainConfig) error {
	deposits := make([]byte, 1) // note: first byte is 0x00 (== deposit request type)
	for _, log := range logs {
		if log.Address == config.DepositContractAddress && len(log.Topics) > 0 && log.Topics[0] == depositTopic {
			request, err := types.DepositLogToRequest(log.Data)
			if err != nil {
				return fmt.Errorf("unable to parse deposit data: %v", err)
			}
			deposits = append(deposits, request...)
		}
	}
	if len(deposits) > 1 {
		*requests = append(*requests, deposits)
	}
	return nil
}

func onSystemCallStart(tracer *tracing.Hooks, ctx *tracing.VMContext) {
	if tracer.OnSystemCallStartV2 != nil {
		tracer.OnSystemCallStartV2(ctx)
	} else if tracer.OnSystemCallStart != nil {
		tracer.OnSystemCallStart()
	}
}
