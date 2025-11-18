// Copyright 2025 The go-ethereum Authors
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
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/urfave/cli/v2"
)

var disasmCommand = &cli.Command{
	Action:      disasmCmd,
	Name:        "disasm",
	Usage:       "Disassembles evm bytecode",
	ArgsUsage:   "<bytecode>",
	Description: `The disasm command disassembles EVM bytecode into human-readable opcodes.`,
	Flags: []cli.Flag{
		CodeFileFlag,
	},
}

func disasmCmd(ctx *cli.Context) error {
	var hexcode string

	codeFileFlag := ctx.String(CodeFileFlag.Name)
	if codeFileFlag == "-" {
		// Read from stdin
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Printf("Could not load code from stdin: %v\n", err)
			os.Exit(1)
		}
		hexcode = string(input)
	} else if codeFileFlag != "" {
		// Read from file
		input, err := os.ReadFile(codeFileFlag)
		if err != nil {
			fmt.Printf("Could not load code from file: %v\n", err)
			os.Exit(1)
		}
		hexcode = string(input)
	} else {
		// Read from command line argument
		hexcode = ctx.Args().First()
		if hexcode == "" {
			fmt.Println("Error: bytecode required as argument or via --codefile")
			os.Exit(1)
		}
	}

	hexcode = strings.TrimSpace(hexcode)
	if len(hexcode)%2 != 0 {
		fmt.Printf("Invalid input length for hex data (%d)\n", len(hexcode))
		os.Exit(1)
	}

	code := common.FromHex(hexcode)
	disassemble(code)
	return nil
}

// disassemble prints the disassembly of EVM bytecode
func disassemble(code []byte) {
	for pc := uint64(0); pc < uint64(len(code)); {
		op := vm.OpCode(code[pc])

		// Print program counter and opcode
		fmt.Printf("%04x: %s", pc, op.String())

		pc++

		// Handle PUSH instructions
		if op >= vm.PUSH1 && op <= vm.PUSH32 {
			size := int(op - vm.PUSH1 + 1)

			// Extract the push data
			if pc+uint64(size) <= uint64(len(code)) {
				data := code[pc : pc+uint64(size)]
				fmt.Printf(" 0x%s", hex.EncodeToString(data))
				pc += uint64(size)
			} else {
				// Incomplete PUSH data
				remaining := code[pc:]
				fmt.Printf(" 0x%s (incomplete, expected %d bytes, got %d)",
					hex.EncodeToString(remaining), size, len(remaining))
				pc = uint64(len(code))
			}
		}

		fmt.Println()
	}
}
