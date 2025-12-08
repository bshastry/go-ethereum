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
	"os"

	"github.com/ethereum/go-ethereum/tests/fuzzers/statetest"
	"github.com/urfave/cli/v2"
)

var dumpCommand = &cli.Command{
	Name:      "dump",
	Usage:     "Dump normalized trace for a single test file",
	ArgsUsage: "<test-file>",
	Action:    dumpAction,
	Flags:     dumpFlags,
	Description: `
The dump command executes a single state test file and outputs the
normalized trace in JSONL format. This is useful for debugging
cross-VM divergences by comparing trace output between clients.

The output includes:
  - _meta: Metadata header with client info, fork, input hash
  - Trace lines: Normalized opcode execution trace
  - _filtered: Filtered entries (if --include-filtered is set)
  - stateRoot: Final state root
  - _result: Summary with traceHash and line count

Example usage:
  statetest-trace dump ./test.json
  statetest-trace dump ./test.json -o trace.jsonl
  statetest-trace dump ./test.json --include-filtered
`,
}

func dumpAction(ctx *cli.Context) error {
	if ctx.NArg() < 1 {
		return fmt.Errorf("usage: statetest-trace dump <test-file>")
	}

	testFile := ctx.Args().Get(0)
	outputPath := ctx.String(OutputFlag.Name)
	includeFiltered := ctx.Bool(IncludeFilteredFlag.Name)
	timeout := ctx.Duration(TimeoutFlag.Name)

	// Read test file
	data, err := os.ReadFile(testFile)
	if err != nil {
		return fmt.Errorf("read test file: %w", err)
	}

	// Validate it's a state test
	if !statetest.IsValidStateTestJSON(data) {
		return fmt.Errorf("not a valid state test: %s", testFile)
	}

	// Configure dump output
	config := &statetest.DumpTraceConfig{
		OutputPath:      outputPath,
		IncludeFiltered: includeFiltered,
	}

	// If no output path, use stdout
	if outputPath == "" {
		// Create a temp file for the dump, then copy to stdout
		tmpFile, err := os.CreateTemp("", "statetest-trace-*.jsonl")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		config.OutputPath = tmpFile.Name()
		tmpFile.Close()
		defer os.Remove(config.OutputPath)
	}

	// Execute and dump trace
	result, err := statetest.ExecuteAndDumpTrace(data, timeout, config)
	if err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	// If we used a temp file, copy to stdout
	if outputPath == "" {
		dumpData, err := os.ReadFile(config.OutputPath)
		if err != nil {
			return fmt.Errorf("read dump: %w", err)
		}
		os.Stdout.Write(dumpData)
	}

	// Print summary to stderr (so it doesn't interfere with stdout output)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintln(os.Stderr, "Trace Dump Summary")
	fmt.Fprintln(os.Stderr, "========================================")
	fmt.Fprintf(os.Stderr, "Trace Hash:  %s\n", result.TraceHash)
	fmt.Fprintf(os.Stderr, "State Root:  %s\n", result.StateRoot)
	fmt.Fprintf(os.Stderr, "Trace Lines: %d\n", result.TraceLines)
	fmt.Fprintf(os.Stderr, "Gas Used:    %d\n", result.GasUsed)
	if outputPath != "" {
		fmt.Fprintf(os.Stderr, "Output:      %s\n", outputPath)
	}
	fmt.Fprintln(os.Stderr, "========================================")

	return nil
}
