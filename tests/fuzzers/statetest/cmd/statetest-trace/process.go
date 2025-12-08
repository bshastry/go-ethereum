// Copyright 2024 The go-ethereum Authors
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

package main

import (
	"fmt"
	"runtime"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest"
	"github.com/urfave/cli/v2"
)

var processCommand = &cli.Command{
	Name:      "process",
	Usage:     "Process a corpus and produce enhanced output with trace hashes",
	ArgsUsage: "<input-dir> <output-dir>",
	Action:    processAction,
	Flags:     processFlags,
	Description: `
The process command reads state test files from the input directory,
executes them with trace normalization, and writes enhanced test files
to the output directory with embedded cross-VM metadata.

The output files include:
  - _info.traceHash: MD5 hash of the normalized execution trace
  - _info.stateRoot: Post-execution state root
  - _info.traceLines: Number of trace lines
  - _info.generatedBy: "geth"
  - _info.generatedAt: ISO timestamp

Example usage:
  statetest-trace process ./corpus ./enhanced_corpus
  statetest-trace process -w 16 -p ./corpus ./enhanced_corpus
  statetest-trace process --fork=Prague ./corpus ./enhanced_corpus
  statetest-trace process --dry-run ./corpus ./enhanced_corpus
`,
}

func processAction(ctx *cli.Context) error {
	if ctx.NArg() < 2 {
		return fmt.Errorf("usage: statetest-trace process <input-dir> <output-dir>")
	}

	inputDir := ctx.Args().Get(0)
	outputDir := ctx.Args().Get(1)

	workers := ctx.Int(WorkersFlag.Name)
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	config := statetest.ProcessConfig{
		Workers:     workers,
		Timeout:     ctx.Duration(TimeoutFlag.Name),
		Fork:        ctx.String(ForkFlag.Name),
		SkipInvalid: ctx.Bool(SkipInvalidFlag.Name),
		Overwrite:   ctx.Bool(OverwriteFlag.Name),
		DryRun:      ctx.Bool(DryRunFlag.Name),
		Progress:    ctx.Bool(ProgressFlag.Name),
		Quiet:       ctx.Bool(QuietFlag.Name),
		Verbose:     ctx.Bool(VerboseFlag.Name),
	}

	processor, err := statetest.NewBatchProcessor(inputDir, outputDir, config)
	if err != nil {
		return fmt.Errorf("create processor: %w", err)
	}

	return processor.Run()
}
