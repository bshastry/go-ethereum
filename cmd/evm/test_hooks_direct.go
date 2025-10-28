//go:build ignore

package main

import (
	"bytes"
	"fmt"
	"log"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/consensus/beacon"
	"github.com/ethereum/go-ethereum/consensus/ethash"
)

func main() {
	fmt.Println("Testing blockchain with tracer...")

	var buf bytes.Buffer
	tracer := logger.NewBlockTracer(&logger.BlockTracerConfig{
		Level: logger.TraceLevelMinimal,
	}, &buf)

	fmt.Printf("Tracer created: %v\n", tracer != nil)
	fmt.Printf("OnBlockStart set: %v\n", tracer.OnBlockStart != nil)

	key, _ := crypto.GenerateKey()
	addr := crypto.PubkeyToAddress(key.PublicKey)

	genesis := &core.Genesis{
		Config:     params.TestChainConfig,
		Timestamp:  100,
		GasLimit:   1000000,
		Difficulty: big.NewInt(0),
		Alloc: types.GenesisAlloc{
			addr: {Balance: big.NewInt(10000000000000000)},
		},
		BaseFee: big.NewInt(10),
	}

	db := rawdb.NewMemoryDatabase()
	tdb := triedb.NewDatabase(db, nil)
	genesis.MustCommit(db, tdb)
	tdb.Close()

	engine := beacon.New(ethash.NewFaker())

	bcCfg := &core.BlockChainConfig{
		StateScheme:    rawdb.PathScheme,
		TrieCleanLimit: 0,
		Preimages:      true,
		VmConfig: vm.Config{
			Tracer: tracer,
		},
	}

	fmt.Printf("BlockChainConfig.VmConfig.Tracer: %v\n", bcCfg.VmConfig.Tracer != nil)

	chain, err := core.NewBlockChain(db, genesis, engine, bcCfg)
	if err != nil {
		log.Fatalf("failed to create blockchain: %v", err)
	}
	defer chain.Stop()

	fmt.Printf("Blockchain created\n")
	fmt.Printf("Buffer size before insertion: %d\n", buf.Len())

	// Try to trigger hooks by inspecting the blockchain internals
	// (This is a test, so we can use reflection if needed)
	fmt.Printf("\nAttempting block insertion...\n")

	// We can't easily test this without reflection, but let's at least verify setup
	fmt.Printf("\nTest setup complete. Buffer size: %d\n", buf.Len())
	fmt.Printf("If buffer size is 0, hooks are not being called\n")
}
