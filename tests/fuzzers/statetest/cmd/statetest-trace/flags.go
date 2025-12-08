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
	"time"

	"github.com/ethereum/go-ethereum/internal/flags"
	"github.com/urfave/cli/v2"
)

const processingCategory = "PROCESSING"

var (
	// WorkersFlag specifies the number of parallel workers
	WorkersFlag = &cli.IntFlag{
		Name:     "workers",
		Aliases:  []string{"w"},
		Usage:    "Number of parallel workers (0 = NumCPU)",
		Value:    0,
		Category: processingCategory,
	}

	// TimeoutFlag specifies the per-test timeout
	TimeoutFlag = &cli.DurationFlag{
		Name:     "timeout",
		Aliases:  []string{"t"},
		Usage:    "Timeout per test execution",
		Value:    30 * time.Second,
		Category: processingCategory,
	}

	// ForkFlag filters tests by fork
	ForkFlag = &cli.StringFlag{
		Name:     "fork",
		Aliases:  []string{"f"},
		Usage:    "Only process tests for specific fork",
		Category: processingCategory,
	}

	// ProgressFlag enables progress reporting
	ProgressFlag = &cli.BoolFlag{
		Name:     "progress",
		Aliases:  []string{"p"},
		Usage:    "Show progress bar",
		Category: processingCategory,
	}

	// SkipInvalidFlag skips invalid tests instead of erroring
	SkipInvalidFlag = &cli.BoolFlag{
		Name:     "skip-invalid",
		Usage:    "Skip invalid tests instead of erroring",
		Category: processingCategory,
	}

	// QuietFlag suppresses non-error output
	QuietFlag = &cli.BoolFlag{
		Name:     "quiet",
		Aliases:  []string{"q"},
		Usage:    "Suppress non-error output",
		Category: flags.LoggingCategory,
	}

	// VerboseFlag enables verbose output
	VerboseFlag = &cli.BoolFlag{
		Name:     "verbose",
		Aliases:  []string{"v"},
		Usage:    "Verbose output (show each processed file)",
		Category: flags.LoggingCategory,
	}

	// DryRunFlag parses and validates without writing
	DryRunFlag = &cli.BoolFlag{
		Name:     "dry-run",
		Usage:    "Parse and validate without writing output",
		Category: processingCategory,
	}

	// OverwriteFlag allows overwriting existing output files
	OverwriteFlag = &cli.BoolFlag{
		Name:     "overwrite",
		Usage:    "Overwrite existing output files",
		Category: processingCategory,
	}

	// OutputFlag specifies the output file path
	OutputFlag = &cli.StringFlag{
		Name:     "output",
		Aliases:  []string{"o"},
		Usage:    "Output file path (default: stdout)",
		Category: processingCategory,
	}

	// IncludeFilteredFlag includes filtered entries in dump output
	IncludeFilteredFlag = &cli.BoolFlag{
		Name:     "include-filtered",
		Usage:    "Include filtered entries (STOP opcodes, depth=0) in trace dump",
		Category: processingCategory,
	}

	// JSONFlag outputs in JSON format
	JSONFlag = &cli.BoolFlag{
		Name:     "json",
		Usage:    "Output in JSON format (machine-readable)",
		Category: processingCategory,
	}
)

// processFlags are flags used by the process command
var processFlags = []cli.Flag{
	WorkersFlag,
	TimeoutFlag,
	ForkFlag,
	ProgressFlag,
	SkipInvalidFlag,
	QuietFlag,
	VerboseFlag,
	DryRunFlag,
	OverwriteFlag,
}

// verifyFlags are flags used by the verify command
var verifyFlags = []cli.Flag{
	WorkersFlag,
	TimeoutFlag,
	VerboseFlag,
	QuietFlag,
}

// dumpFlags are flags used by the dump command
var dumpFlags = []cli.Flag{
	OutputFlag,
	IncludeFilteredFlag,
	TimeoutFlag,
}

// statsFlags are flags used by the stats command
var statsFlags = []cli.Flag{
	JSONFlag,
	QuietFlag,
}
