// Copyright 2014 The go-ethereum Authors
// This file is part of go-ethereum.
//
// go-ethereum is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// go-ethereum is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with go-ethereum. If not, see <http://www.gnu.org/licenses/>.

// evm executes EVM code snippets.
package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/cmd/evm/internal/t8ntool"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/internal/debug"
	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/urfave/cli/v2"

	// Force-load the tracer engines to trigger registration
	_ "github.com/ethereum/go-ethereum/eth/tracers/js"
	_ "github.com/ethereum/go-ethereum/eth/tracers/native"
)

// Some other nice-to-haves:
// * accumulate traces into an object to bundle with test
// * write tx identifier for trace before hand (blocktest only)
// * combine blocktest and statetest runner logic using unified test interface

const traceCategory = "TRACING"

var (
	// Test running flags.
	RunFlag = &cli.StringFlag{
		Name:  "run",
		Value: ".*",
		Usage: "Run only those tests matching the regular expression.",
	}
	BenchFlag = &cli.BoolFlag{
		Name:     "bench",
		Usage:    "benchmark the execution",
		Category: flags.VMCategory,
	}
	WitnessCrossCheckFlag = &cli.BoolFlag{
		Name:    "cross-check",
		Aliases: []string{"xc"},
		Usage:   "Cross-check stateful execution against stateless, verifying the witness generation.",
	}

	// Debugging flags.
	DumpFlag = &cli.BoolFlag{
		Name:  "dump",
		Usage: "dumps the state after the run",
	}
	HumanReadableFlag = &cli.BoolFlag{
		Name:  "human",
		Usage: "\"Human-readable\" output",
	}
	StatDumpFlag = &cli.BoolFlag{
		Name:  "statdump",
		Usage: "displays stack and heap memory information",
	}

	// Tracing flags.
	TraceFlag = &cli.BoolFlag{
		Name:     "trace",
		Usage:    "Enable tracing and output trace log.",
		Category: traceCategory,
	}
	TraceFormatFlag = &cli.StringFlag{
		Name:     "trace.format",
		Usage:    "Trace output format to use (json|struct|md)",
		Value:    "json",
		Category: traceCategory,
	}
	TraceDisableMemoryFlag = &cli.BoolFlag{
		Name:     "trace.nomemory",
		Aliases:  []string{"nomemory"},
		Value:    true,
		Usage:    "disable memory output",
		Category: traceCategory,
	}
	TraceDisableStackFlag = &cli.BoolFlag{
		Name:     "trace.nostack",
		Aliases:  []string{"nostack"},
		Usage:    "disable stack output",
		Category: traceCategory,
	}
	TraceDisableStorageFlag = &cli.BoolFlag{
		Name:     "trace.nostorage",
		Aliases:  []string{"nostorage"},
		Usage:    "disable storage output",
		Category: traceCategory,
	}
	TraceDisableReturnDataFlag = &cli.BoolFlag{
		Name:     "trace.noreturndata",
		Aliases:  []string{"noreturndata"},
		Value:    true,
		Usage:    "enable return data output",
		Category: traceCategory,
	}

	// Block-level tracing flags (EIP Block-Level Execution Trace Specification).
	TraceBlockLevelFlag = &cli.BoolFlag{
		Name:     "trace.blocklevel",
		Usage:    "Enable block-level tracing (extends EIP-3155 to include block-level operations)",
		Category: traceCategory,
	}
	TraceBlockLevelLevelFlag = &cli.StringFlag{
		Name:     "trace.blocklevel.level",
		Usage:    "Block trace level: minimal (block/tx boundaries), standard (+pre/post execution, validation), full (+opcodes, trie ops), custom (user-specified)",
		Value:    "standard",
		Category: traceCategory,
	}
	TraceBlockLevelTypesFlag = &cli.StringFlag{
		Name:     "trace.blocklevel.types",
		Usage:    "Custom trace types (comma-separated): blockStart,txStart,preExecution,postExecution,validation,trieOperation,operation,txEnd,blockEnd",
		Category: traceCategory,
	}
	TraceBlockLevelOpcodeFlag = &cli.BoolFlag{
		Name:     "trace.blocklevel.opcode",
		Usage:    "Include EIP-3155 opcodes in full mode (default: true)",
		Value:    true,
		Category: traceCategory,
	}
	TraceBlockLevelTrieFlag = &cli.BoolFlag{
		Name:     "trace.blocklevel.trie",
		Usage:    "Include trie operations in full mode (default: true)",
		Value:    true,
		Category: traceCategory,
	}
	TraceBlockLevelIncludeTxFlag = &cli.BoolFlag{
		Name:     "trace.blocklevel.include-tx",
		Usage:    "Include transaction-level traces (txStart, txEnd, operation)",
		Value:    true,
		Category: traceCategory,
	}
	TraceBlockLevelIncludeBlockFlag = &cli.BoolFlag{
		Name:     "trace.blocklevel.include-block",
		Usage:    "Include block-level traces (blockStart, blockEnd, preExecution, postExecution, validation, trieOperation)",
		Value:    true,
		Category: traceCategory,
	}

	// Deprecated flags.
	DebugFlag = &cli.BoolFlag{
		Name:     "debug",
		Usage:    "output full trace logs (deprecated)",
		Hidden:   true,
		Category: traceCategory,
	}
	MachineFlag = &cli.BoolFlag{
		Name:     "json",
		Usage:    "output trace logs in machine readable format, json (deprecated)",
		Hidden:   true,
		Category: traceCategory,
	}
)

// Command definitions.
var (
	stateTransitionCommand = &cli.Command{
		Name:    "transition",
		Aliases: []string{"t8n"},
		Usage:   "Executes a full state transition",
		Action:  t8ntool.Transition,
		Flags: []cli.Flag{
			t8ntool.TraceFlag,
			t8ntool.TraceTracerFlag,
			t8ntool.TraceTracerConfigFlag,
			t8ntool.TraceEnableMemoryFlag,
			t8ntool.TraceDisableStackFlag,
			t8ntool.TraceEnableReturnDataFlag,
			t8ntool.TraceEnableCallFramesFlag,
			t8ntool.OutputBasedir,
			t8ntool.OutputAllocFlag,
			t8ntool.OutputResultFlag,
			t8ntool.OutputBodyFlag,
			t8ntool.InputAllocFlag,
			t8ntool.InputEnvFlag,
			t8ntool.InputTxsFlag,
			t8ntool.ForknameFlag,
			t8ntool.ChainIDFlag,
			t8ntool.RewardFlag,
		},
	}
	transactionCommand = &cli.Command{
		Name:    "transaction",
		Aliases: []string{"t9n"},
		Usage:   "Performs transaction validation",
		Action:  t8ntool.Transaction,
		Flags: []cli.Flag{
			t8ntool.InputTxsFlag,
			t8ntool.ChainIDFlag,
			t8ntool.ForknameFlag,
		},
	}

	blockBuilderCommand = &cli.Command{
		Name:    "block-builder",
		Aliases: []string{"b11r"},
		Usage:   "Builds a block",
		Action:  t8ntool.BuildBlock,
		Flags: []cli.Flag{
			t8ntool.OutputBasedir,
			t8ntool.OutputBlockFlag,
			t8ntool.InputHeaderFlag,
			t8ntool.InputOmmersFlag,
			t8ntool.InputWithdrawalsFlag,
			t8ntool.InputTxsRlpFlag,
			t8ntool.SealCliqueFlag,
		},
	}
)

// traceFlags contains flags that configure tracing output.
var traceFlags = []cli.Flag{
	TraceFlag,
	TraceFormatFlag,
	TraceDisableStackFlag,
	TraceDisableMemoryFlag,
	TraceDisableStorageFlag,
	TraceDisableReturnDataFlag,

	// Block-level tracing flags
	TraceBlockLevelFlag,
	TraceBlockLevelLevelFlag,
	TraceBlockLevelTypesFlag,
	TraceBlockLevelOpcodeFlag,
	TraceBlockLevelTrieFlag,
	TraceBlockLevelIncludeTxFlag,
	TraceBlockLevelIncludeBlockFlag,

	// deprecated
	DebugFlag,
	MachineFlag,
}

var app = flags.NewApp("the evm command line interface")

func init() {
	app.Flags = debug.Flags
	app.Commands = []*cli.Command{
		runCommand,
		blockTestCommand,
		stateTestCommand,
		stateTransitionCommand,
		transactionCommand,
		blockBuilderCommand,
		disasmCommand,
	}
	app.Before = func(ctx *cli.Context) error {
		flags.MigrateGlobalFlags(ctx)
		return debug.Setup(ctx)
	}
	app.After = func(ctx *cli.Context) error {
		debug.Exit()
		return nil
	}
}

func main() {
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// tracerFromFlags parses the cli flags and returns the specified tracer.
// The forkName parameter is optional and should be provided when the fork is
// known externally (e.g., from blocktests). If empty, fork detection will be
// based on block header features.
func tracerFromFlags(ctx *cli.Context, forkName ...string) *tracing.Hooks {
	config := &logger.Config{
		EnableMemory:     !ctx.Bool(TraceDisableMemoryFlag.Name),
		DisableStack:     ctx.Bool(TraceDisableStackFlag.Name),
		DisableStorage:   ctx.Bool(TraceDisableStorageFlag.Name),
		EnableReturnData: !ctx.Bool(TraceDisableReturnDataFlag.Name),
	}

	// Check for block-level tracing first (highest priority)
	if ctx.Bool(TraceFlag.Name) && ctx.Bool(TraceBlockLevelFlag.Name) {
		// Block-level tracing enabled
		blockCfg := &logger.BlockTracerConfig{
			Config:        config,
			IncludeOpcode: ctx.Bool(TraceBlockLevelOpcodeFlag.Name),
			IncludeTrie:   ctx.Bool(TraceBlockLevelTrieFlag.Name),
			IncludeTx:     ctx.Bool(TraceBlockLevelIncludeTxFlag.Name),
			IncludeBlock:  ctx.Bool(TraceBlockLevelIncludeBlockFlag.Name),
		}

		// If fork name was provided (e.g., from blocktest), use it
		if len(forkName) > 0 && forkName[0] != "" {
			blockCfg.ForkName = forkName[0]
		}

		// Parse trace level
		levelStr := ctx.String(TraceBlockLevelLevelFlag.Name)
		switch levelStr {
		case "minimal":
			blockCfg.Level = logger.TraceLevelMinimal
		case "standard":
			blockCfg.Level = logger.TraceLevelStandard
		case "full":
			blockCfg.Level = logger.TraceLevelFull
		case "custom":
			blockCfg.Level = logger.TraceLevelCustom
			// Parse custom types (comma-separated)
			if typesStr := ctx.String(TraceBlockLevelTypesFlag.Name); typesStr != "" {
				blockCfg.CustomTypes = strings.Split(typesStr, ",")
			}
		default:
			fmt.Fprintf(os.Stderr, "unknown block trace level: %q (must be: minimal, standard, full, or custom)\n", levelStr)
			os.Exit(1)
			return nil
		}

		return logger.NewBlockTracer(blockCfg, os.Stderr).Hooks
	}

	// Standard EIP-3155 tracing
	switch {
	case ctx.Bool(TraceFlag.Name):
		switch format := ctx.String(TraceFormatFlag.Name); format {
		case "struct":
			return logger.NewStreamingStructLogger(config, os.Stderr).Hooks()
		case "json":
			return logger.NewJSONLogger(config, os.Stderr)
		case "md", "markdown":
			return logger.NewMarkdownLogger(config, os.Stderr).Hooks()
		default:
			fmt.Fprintf(os.Stderr, "unknown trace format: %q\n", format)
			os.Exit(1)
			return nil
		}
	// Deprecated ways of configuring tracing.
	case ctx.Bool(MachineFlag.Name):
		return logger.NewJSONLogger(config, os.Stderr)
	case ctx.Bool(DebugFlag.Name):
		return logger.NewStreamingStructLogger(config, os.Stderr).Hooks()
	default:
		return nil
	}
}

// collectFiles walks the given path. If the path is a directory, it will
// return a list of all accumulates all files with json extension.
// Otherwise (if path points to a file), it will return the path.
func collectFiles(path string) []string {
	var out []string
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		// User explicitly pointed out a file, ignore extension.
		return []string{path}
	}
	err := filepath.Walk(path, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(info.Name()) == ".json" {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return out
}

// dump returns a state dump for the most current trie.
func dump(s *state.StateDB) *state.Dump {
	root := s.IntermediateRoot(false)
	cpy, _ := state.New(root, s.Database())
	dump := cpy.RawDump(nil)
	return &dump
}
