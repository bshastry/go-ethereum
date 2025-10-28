package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestBlockTracerIntegration(t *testing.T) {
	testCases := []struct {
		name           string
		chainConfig    *params.ChainConfig
		traceLevel     logger.TraceLevel
		includeOpcode  bool
		includeTrie    bool
		customTypes    []string
		withWithdrawal bool
		expectedTypes  []string
	}{
		{
			name:          "MinimalLevel_Shanghai",
			chainConfig:   shanghaiConfig(),
			traceLevel:    logger.TraceLevelMinimal,
			expectedTypes: []string{"blockStart", "txStart", "txEnd", "blockEnd"},
		},
		{
			name:        "StandardLevel_Shanghai_WithWithdrawals",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelStandard,
			expectedTypes: []string{
				"blockStart", "txStart", "txEnd",
				"postExecution", "validation", "blockEnd",
			},
			withWithdrawal: true,
		},
		{
			name:          "FullLevel_Shanghai_WithOpcodes",
			chainConfig:   shanghaiConfig(),
			traceLevel:    logger.TraceLevelFull,
			includeOpcode: true,
			includeTrie:   false,
			expectedTypes: []string{
				"blockStart", "txStart", "txEnd",
				"postExecution", "validation", "blockEnd",
			},
		},
		{
			name:          "FullLevel_Shanghai_WithTrie",
			chainConfig:   shanghaiConfig(),
			traceLevel:    logger.TraceLevelFull,
			includeOpcode: false,
			includeTrie:   true,
			expectedTypes: []string{
				"blockStart", "txStart", "txEnd",
				"postExecution", "validation", "trieOperation", "blockEnd",
			},
			withWithdrawal: true,
		},
		{
			name:        "CustomLevel_OnlyBlockBoundaries",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelCustom,
			customTypes: []string{"blockStart", "blockEnd"},
			expectedTypes: []string{
				"blockStart", "blockEnd",
			},
		},
		{
			name:        "CustomLevel_TxAndValidation",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelCustom,
			customTypes: []string{"txStart", "txEnd", "validation"},
			expectedTypes: []string{
				"txStart", "txEnd", "validation",
			},
		},
		{
			name:          "Cancun_WithBlobGas",
			chainConfig:   cancunConfig(),
			traceLevel:    logger.TraceLevelStandard,
			withWithdrawal: true,
			expectedTypes: []string{
				"blockStart", "txStart", "txEnd",
				"postExecution", "validation", "blockEnd",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &logger.BlockTracerConfig{
				Config:        &logger.Config{},
				Level:         tc.traceLevel,
				IncludeOpcode: tc.includeOpcode,
				IncludeTrie:   tc.includeTrie,
				CustomTypes:   tc.customTypes,
			}

			tracer := logger.NewBlockTracer(cfg, &buf)

			key, _ := crypto.GenerateKey()
			addr := crypto.PubkeyToAddress(key.PublicKey)

			genesis := &core.Genesis{
				Config:    tc.chainConfig,
				Timestamp: 100,
				GasLimit:  1000000,
				Difficulty: big.NewInt(0),
				Alloc: types.GenesisAlloc{
					addr: {Balance: big.NewInt(10000000000000000)},
				},
				BaseFee:       big.NewInt(10),
				ExcessBlobGas: new(uint64),
				BlobGasUsed:   new(uint64),
			}

			db := rawdb.NewMemoryDatabase()
			tdb := triedb.NewDatabase(db, nil)
			gblock := genesis.MustCommit(db, tdb)
			tdb.Close()

			engine := beacon.New(ethash.NewFaker())

			chain, err := core.NewBlockChain(db, genesis, engine, &core.BlockChainConfig{
				StateScheme:    rawdb.PathScheme,
				TrieCleanLimit: 0,
				Preimages:      true,
				VmConfig: vm.Config{
					Tracer: tracer.Hooks,
				},
			})
			if err != nil {
				t.Fatalf("failed to create blockchain: %v", err)
			}
			defer chain.Stop()

			signer := types.LatestSigner(tc.chainConfig)
			tx := types.MustSignNewTx(key, signer, &types.DynamicFeeTx{
				ChainID:   tc.chainConfig.ChainID,
				Nonce:     0,
				GasTipCap: big.NewInt(1),
				GasFeeCap: big.NewInt(100000),
				Gas:       21000,
				To:        &common.Address{0x1},
				Value:     big.NewInt(1),
			})

			var withdrawals []*types.Withdrawal
			if tc.withWithdrawal {
				withdrawals = []*types.Withdrawal{
					{
						Index:     0,
						Validator: 1,
						Address:   common.Address{0x2},
						Amount:    1000000000,
					},
				}
			} else if tc.chainConfig.ShanghaiTime != nil && *tc.chainConfig.ShanghaiTime == 0 {
				// For Shanghai blocks, use empty slice instead of nil
				withdrawals = []*types.Withdrawal{}
			}

			header := &types.Header{
				ParentHash:  gblock.Hash(),
				Number:      big.NewInt(1),
				GasLimit:    1000000,
				Time:        200,
				Difficulty:  big.NewInt(0),
				Coinbase:    common.Address{},
				BaseFee:     big.NewInt(9),
			}

			// For Shanghai blocks, always include withdrawals root (even if empty)
			if tc.chainConfig.ShanghaiTime != nil && *tc.chainConfig.ShanghaiTime == 0 {
				var withdrawalsHash common.Hash
				if len(withdrawals) > 0 {
					withdrawalsHash = types.DeriveSha(types.Withdrawals(withdrawals), trie.NewStackTrie(nil))
				} else {
					withdrawalsHash = types.EmptyWithdrawalsHash
				}
				header.WithdrawalsHash = &withdrawalsHash
			}

			// For Cancun blocks, add blob gas fields
			if tc.chainConfig.CancunTime != nil && *tc.chainConfig.CancunTime == 0 {
				blobGasUsed := uint64(0)
				excessBlobGas := uint64(0)
				header.BlobGasUsed = &blobGasUsed
				header.ExcessBlobGas = &excessBlobGas
			}

			block := types.NewBlock(header, &types.Body{
				Transactions: []*types.Transaction{tx},
				Withdrawals:  withdrawals,
			}, nil, trie.NewStackTrie(nil))

			_, err = chain.InsertChain(types.Blocks{block})
			if err != nil {
				t.Fatalf("block insertion failed: %v", err)
			}

			// Debug: print buffer contents
			t.Logf("Buffer size: %d bytes", buf.Len())
			if buf.Len() > 0 {
				t.Logf("Buffer contents:\n%s", buf.String())
			}

			records := parseJSONLines(t, buf.Bytes())

			if len(records) == 0 {
				t.Fatalf("no trace records emitted (buffer size: %d)", buf.Len())
			}

			verifyRecordTypes(t, records, tc.expectedTypes)
			verifyRecordFields(t, records, tc)
		})
	}
}

func TestBlockTracerRecordContent(t *testing.T) {
	testCases := []struct {
		name        string
		chainConfig *params.ChainConfig
		traceLevel  logger.TraceLevel
		checks      func(t *testing.T, records []map[string]interface{})
	}{
		{
			name:        "BlockStart_VerifyFields",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelMinimal,
			checks: func(t *testing.T, records []map[string]interface{}) {
				blockStart := findRecordByType(records, "blockStart")
				if blockStart == nil {
					t.Fatal("blockStart record not found")
				}

				requiredFields := []string{
					"blockNumber", "blockHash", "parentHash", "timestamp",
					"gasLimit", "difficulty", "miner",
				}
				for _, field := range requiredFields {
					if _, ok := blockStart[field]; !ok {
						t.Errorf("blockStart missing required field: %s", field)
					}
				}

				if blockNum, ok := blockStart["blockNumber"].(string); !ok || blockNum == "" {
					t.Error("blockStart blockNumber invalid")
				}
			},
		},
		{
			name:        "TxStart_VerifyFields",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelMinimal,
			checks: func(t *testing.T, records []map[string]interface{}) {
				txStart := findRecordByType(records, "txStart")
				if txStart == nil {
					t.Fatal("txStart record not found")
				}

				requiredFields := []string{
					"txIndex", "txHash", "txType", "from",
					"gasLimit", "nonce", "value",
				}
				for _, field := range requiredFields {
					if _, ok := txStart[field]; !ok {
						t.Errorf("txStart missing required field: %s", field)
					}
				}

				if txIdx, ok := txStart["txIndex"].(string); !ok || txIdx != "0x0" {
					t.Errorf("txStart txIndex invalid: %v", txStart["txIndex"])
				}
			},
		},
		{
			name:        "TxEnd_VerifyReceipt",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelMinimal,
			checks: func(t *testing.T, records []map[string]interface{}) {
				txEnd := findRecordByType(records, "txEnd")
				if txEnd == nil {
					t.Fatal("txEnd record not found")
				}

				requiredFields := []string{
					"txIndex", "txHash", "gasUsed", "cumulativeGasUsed", "status",
				}
				for _, field := range requiredFields {
					if _, ok := txEnd[field]; !ok {
						t.Errorf("txEnd missing required field: %s", field)
					}
				}

				status, ok := txEnd["status"].(string)
				if !ok || (status != "0x1" && status != "0x0") {
					t.Errorf("txEnd status invalid: %v", txEnd["status"])
				}
			},
		},
		{
			name:        "BlockEnd_VerifyRoots",
			chainConfig: shanghaiConfig(),
			traceLevel:  logger.TraceLevelMinimal,
			checks: func(t *testing.T, records []map[string]interface{}) {
				blockEnd := findRecordByType(records, "blockEnd")
				if blockEnd == nil {
					t.Fatal("blockEnd record not found")
				}

				requiredFields := []string{
					"blockNumber", "blockHash", "stateRoot",
					"transactionsRoot", "receiptsRoot", "validationResult",
				}
				for _, field := range requiredFields {
					if _, ok := blockEnd[field]; !ok {
						t.Errorf("blockEnd missing required field: %s", field)
					}
				}

				validationResult, ok := blockEnd["validationResult"].(string)
				if !ok || validationResult != "valid" {
					t.Errorf("blockEnd validationResult should be 'valid', got: %v", blockEnd["validationResult"])
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := &logger.BlockTracerConfig{
				Config:     &logger.Config{},
				Level:      tc.traceLevel,
				IncludeTrie: false,
			}

			tracer := logger.NewBlockTracer(cfg, &buf)

			key, _ := crypto.GenerateKey()
			addr := crypto.PubkeyToAddress(key.PublicKey)

			genesis := &core.Genesis{
				Config:    tc.chainConfig,
				Timestamp: 100,
				GasLimit:  1000000,
				Difficulty: big.NewInt(0),
				Alloc: types.GenesisAlloc{
					addr: {Balance: big.NewInt(10000000000000000)},
				},
				BaseFee: big.NewInt(10),
			}

			db := rawdb.NewMemoryDatabase()
			tdb := triedb.NewDatabase(db, nil)
			gblock := genesis.MustCommit(db, tdb)
			tdb.Close()

			engine := beacon.New(ethash.NewFaker())

			chain, err := core.NewBlockChain(db, genesis, engine, &core.BlockChainConfig{
				StateScheme:    rawdb.PathScheme,
				TrieCleanLimit: 0,
				Preimages:      true,
				VmConfig: vm.Config{
					Tracer: tracer.Hooks,
				},
			})
			if err != nil {
				t.Fatalf("failed to create blockchain: %v", err)
			}
			defer chain.Stop()

			signer := types.LatestSigner(tc.chainConfig)
			tx := types.MustSignNewTx(key, signer, &types.DynamicFeeTx{
				ChainID:   tc.chainConfig.ChainID,
				Nonce:     0,
				GasTipCap: big.NewInt(1),
				GasFeeCap: big.NewInt(100000),
				Gas:       21000,
				To:        &common.Address{0x1},
				Value:     big.NewInt(1),
			})

			header := &types.Header{
				ParentHash:  gblock.Hash(),
				Number:      big.NewInt(1),
				GasLimit:    1000000,
				Time:        200,
				Difficulty:  big.NewInt(0),
				Coinbase:    common.Address{},
				BaseFee:     big.NewInt(9),
			}

			// For Shanghai blocks, always include withdrawals root (even if empty)
			if tc.chainConfig.ShanghaiTime != nil && *tc.chainConfig.ShanghaiTime == 0 {
				withdrawalsHash := types.EmptyWithdrawalsHash
				header.WithdrawalsHash = &withdrawalsHash
			}

			block := types.NewBlock(header, &types.Body{Transactions: []*types.Transaction{tx}}, nil, trie.NewStackTrie(nil))

			_, err = chain.InsertChain(types.Blocks{block})
			if err != nil {
				t.Fatalf("block insertion failed: %v", err)
			}

			records := parseJSONLines(t, buf.Bytes())

			tc.checks(t, records)
		})
	}
}

func parseJSONLines(t *testing.T, data []byte) []map[string]interface{} {
	t.Helper()

	var records []map[string]interface{}
	scanner := bufio.NewScanner(bytes.NewReader(data))

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var record map[string]interface{}
		if err := json.Unmarshal(line, &record); err != nil {
			t.Logf("warning: failed to parse JSON line: %v", err)
			continue
		}

		records = append(records, record)
	}

	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner error: %v", err)
	}

	return records
}

func verifyRecordTypes(t *testing.T, records []map[string]interface{}, expectedTypes []string) {
	t.Helper()

	foundTypes := make(map[string]bool)
	for _, record := range records {
		if typ, ok := record["type"].(string); ok {
			foundTypes[typ] = true
		}
	}

	for _, expectedType := range expectedTypes {
		if !foundTypes[expectedType] {
			t.Errorf("expected record type %q not found in trace output", expectedType)
		}
	}
}

func verifyRecordFields(t *testing.T, records []map[string]interface{}, tc struct {
	name           string
	chainConfig    *params.ChainConfig
	traceLevel     logger.TraceLevel
	includeOpcode  bool
	includeTrie    bool
	customTypes    []string
	withWithdrawal bool
	expectedTypes  []string
}) {
	t.Helper()

	for _, record := range records {
		typ, ok := record["type"].(string)
		if !ok {
			t.Error("record missing type field")
			continue
		}

		switch typ {
		case "blockStart":
			if _, ok := record["blockNumber"]; !ok {
				t.Error("blockStart missing blockNumber")
			}
			if _, ok := record["timestamp"]; !ok {
				t.Error("blockStart missing timestamp")
			}
			if tc.chainConfig.CancunTime != nil && *tc.chainConfig.CancunTime == 0 {
				if _, ok := record["excessBlobGas"]; !ok {
					t.Error("Cancun blockStart should include excessBlobGas")
				}
			}

		case "txStart":
			if _, ok := record["txHash"]; !ok {
				t.Error("txStart missing txHash")
			}
			if _, ok := record["from"]; !ok {
				t.Error("txStart missing from")
			}

		case "txEnd":
			if _, ok := record["gasUsed"]; !ok {
				t.Error("txEnd missing gasUsed")
			}
			if _, ok := record["status"]; !ok {
				t.Error("txEnd missing status")
			}

		case "blockEnd":
			if _, ok := record["stateRoot"]; !ok {
				t.Error("blockEnd missing stateRoot")
			}
			if _, ok := record["validationResult"]; !ok {
				t.Error("blockEnd missing validationResult")
			}

		case "postExecution":
			if _, ok := record["operation"]; !ok {
				t.Error("postExecution missing operation")
			}

		case "validation":
			if _, ok := record["operation"]; !ok {
				t.Error("validation missing operation")
			}

		case "trieOperation":
			if _, ok := record["operation"]; !ok {
				t.Error("trieOperation missing operation")
			}
		}
	}
}

func findRecordByType(records []map[string]interface{}, typ string) map[string]interface{} {
	for _, record := range records {
		if t, ok := record["type"].(string); ok && t == typ {
			return record
		}
	}
	return nil
}

func findRecordsByType(records []map[string]interface{}, typ string) []map[string]interface{} {
	var result []map[string]interface{}
	for _, record := range records {
		if t, ok := record["type"].(string); ok && t == typ {
			result = append(result, record)
		}
	}
	return result
}

func shanghaiConfig() *params.ChainConfig {
	config := *params.TestChainConfig
	config.ShanghaiTime = new(uint64)
	*config.ShanghaiTime = 0
	return &config
}

func cancunConfig() *params.ChainConfig {
	config := *params.TestChainConfig
	config.ShanghaiTime = new(uint64)
	*config.ShanghaiTime = 0
	config.CancunTime = new(uint64)
	*config.CancunTime = 0
	return &config
}
