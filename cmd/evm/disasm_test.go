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
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestDisassemble(t *testing.T) {
	tests := []struct {
		name     string
		bytecode string
		want     []string
	}{
		{
			name:     "simple push and add",
			bytecode: "6001600201",
			want: []string{
				"PUSH1 0x01",
				"PUSH1 0x02",
				"ADD",
			},
		},
		{
			name:     "push and store",
			bytecode: "600160025500",
			want: []string{
				"PUSH1 0x01",
				"PUSH1 0x02",
				"SSTORE",
				"STOP",
			},
		},
		{
			name:     "push32",
			bytecode: "7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			want: []string{
				"PUSH32 0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			},
		},
		{
			name:     "incomplete push",
			bytecode: "6001600260", // PUSH1 0x01, PUSH1 0x02, PUSH1 (incomplete)
			want: []string{
				"PUSH1 0x01",
				"PUSH1 0x02",
				"PUSH1", // incomplete
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := common.FromHex(tt.bytecode)

			// Capture stdout
			old := os.Stdout
			r, w, _ := os.Pipe()
			os.Stdout = w

			disassemble(code)

			w.Close()
			os.Stdout = old

			var buf bytes.Buffer
			io.Copy(&buf, r)
			output := buf.String()

			for _, want := range tt.want {
				if !strings.Contains(output, want) {
					t.Errorf("Expected output to contain %q, got:\n%s", want, output)
				}
			}
		})
	}
}

func TestDisassemblePUSH30(t *testing.T) {
	// Test the specific bytecode from the user's example
	bytecode := "7d0153136b7be165e4a393d17a238323694af8d317c046144c8a86d9e18927"
	code := common.FromHex(bytecode)

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	disassemble(code)

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "PUSH30") {
		t.Errorf("Expected output to contain PUSH30, got:\n%s", output)
	}
}
