// Copyright 2024 The go-ethereum Authors
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

package mutations

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
)

// BytecodeStrategy performs basic bytecode mutation while preserving PUSH operands.
type BytecodeStrategy struct {
	rng *rand.Rand
}

// NewBytecodeStrategy creates a new bytecode mutation strategy.
func NewBytecodeStrategy() *BytecodeStrategy {
	return &BytecodeStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *BytecodeStrategy) Name() string { return "bytecode" }

// Description returns a brief description of this strategy.
func (s *BytecodeStrategy) Description() string {
	return "Basic bytecode mutation (flip, inc/dec, bit flip)"
}

// Weight returns the relative weight for this strategy.
func (s *BytecodeStrategy) Weight() int { return 10 }

// Mutate performs mutation on raw JSON test data.
func (s *BytecodeStrategy) Mutate(data []byte) ([]byte, string, error) {
	return mutateCodeInTest(data, s.rng, s.mutateCode)
}

func (s *BytecodeStrategy) mutateCode(code []byte) []byte {
	if len(code) == 0 {
		return code
	}

	mutated := make([]byte, len(code))
	copy(mutated, code)

	// Find mutable positions (not PUSH operands)
	mutable := findMutablePositions(code)
	if len(mutable) == 0 {
		pos := s.rng.Intn(len(mutated))
		mutated[pos] = byte(s.rng.Intn(256))
		return mutated
	}

	pos := mutable[s.rng.Intn(len(mutable))]
	strategy := s.rng.Intn(100)

	switch {
	case strategy < 50:
		mutated[pos] = byte(s.rng.Intn(256))
	case strategy < 80:
		if s.rng.Intn(2) == 0 {
			mutated[pos]++
		} else {
			mutated[pos]--
		}
	default:
		bitPos := s.rng.Intn(8)
		mutated[pos] ^= (1 << bitPos)
	}

	return mutated
}

// OpcodeSmartStrategy performs opcode-aware mutations.
// It knows about opcode semantics and mutates to related opcodes.
type OpcodeSmartStrategy struct {
	rng *rand.Rand
}

// NewOpcodeSmartStrategy creates a new opcode-aware mutation strategy.
func NewOpcodeSmartStrategy() *OpcodeSmartStrategy {
	return &OpcodeSmartStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *OpcodeSmartStrategy) Name() string { return "opcode-smart" }

// Description returns a brief description of this strategy.
func (s *OpcodeSmartStrategy) Description() string {
	return "Opcode-aware mutation (replace with related opcodes)"
}

// Weight returns the relative weight for this strategy.
func (s *OpcodeSmartStrategy) Weight() int { return 15 }

// opcodeGroups defines related opcode groups for smart mutation.
// Mutating within groups tests edge cases between similar operations.
var opcodeGroups = map[byte][]byte{
	// Arithmetic
	0x01: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // ADD -> arithmetic
	0x02: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // MUL -> arithmetic
	0x03: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // SUB -> arithmetic
	0x04: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // DIV -> arithmetic
	0x05: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // SDIV -> arithmetic
	0x06: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // MOD -> arithmetic
	0x07: {0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0A, 0x0B}, // SMOD -> arithmetic

	// Comparison
	0x10: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15}, // LT, GT, SLT, SGT, EQ, ISZERO
	0x11: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15},
	0x12: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15},
	0x13: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15},
	0x14: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15},
	0x15: {0x10, 0x11, 0x12, 0x13, 0x14, 0x15},

	// Bitwise
	0x16: {0x16, 0x17, 0x18, 0x19}, // AND, OR, XOR, NOT
	0x17: {0x16, 0x17, 0x18, 0x19},
	0x18: {0x16, 0x17, 0x18, 0x19},
	0x19: {0x16, 0x17, 0x18, 0x19},

	// Memory
	0x51: {0x51, 0x52, 0x53}, // MLOAD, MSTORE, MSTORE8
	0x52: {0x51, 0x52, 0x53},
	0x53: {0x51, 0x52, 0x53},

	// Storage
	0x54: {0x54, 0x55}, // SLOAD, SSTORE
	0x55: {0x54, 0x55},

	// Transient storage (EIP-1153)
	0x5C: {0x5C, 0x5D}, // TLOAD, TSTORE
	0x5D: {0x5C, 0x5D},

	// Call variants
	0xF1: {0xF1, 0xF2, 0xF4, 0xFA}, // CALL, CALLCODE, DELEGATECALL, STATICCALL
	0xF2: {0xF1, 0xF2, 0xF4, 0xFA},
	0xF4: {0xF1, 0xF2, 0xF4, 0xFA},
	0xFA: {0xF1, 0xF2, 0xF4, 0xFA},

	// Create variants
	0xF0: {0xF0, 0xF5}, // CREATE, CREATE2
	0xF5: {0xF0, 0xF5},

	// Return variants
	0xF3: {0xF3, 0xFD}, // RETURN, REVERT
	0xFD: {0xF3, 0xFD},

	// MCOPY (EIP-5656, Cancun)
	0x5E: {0x5E, 0x37, 0x39}, // MCOPY, CALLDATACOPY, CODECOPY

	// BLOBHASH (EIP-4844, Cancun)
	0x49: {0x49, 0x40, 0x41, 0x42, 0x43}, // BLOBHASH with block info opcodes

	// BLOBBASEFEE (EIP-7516, Cancun)
	0x4A: {0x4A, 0x48, 0x3A}, // BLOBBASEFEE, BASEFEE, GASPRICE
}

// Mutate performs mutation on raw JSON test data.
func (s *OpcodeSmartStrategy) Mutate(data []byte) ([]byte, string, error) {
	return mutateCodeInTest(data, s.rng, s.mutateCode)
}

func (s *OpcodeSmartStrategy) mutateCode(code []byte) []byte {
	if len(code) == 0 {
		return code
	}

	mutated := make([]byte, len(code))
	copy(mutated, code)

	mutable := findMutablePositions(code)
	if len(mutable) == 0 {
		return mutated
	}

	pos := mutable[s.rng.Intn(len(mutable))]
	opcode := code[pos]

	// Check if we have related opcodes
	if related, ok := opcodeGroups[opcode]; ok && len(related) > 1 {
		// Pick a different opcode from the same group
		for {
			newOp := related[s.rng.Intn(len(related))]
			if newOp != opcode {
				mutated[pos] = newOp
				break
			}
		}
	} else {
		// Fall back to random mutation
		mutated[pos] = byte(s.rng.Intn(256))
	}

	return mutated
}

// Helper functions shared by mutation strategies

// findMutablePositions returns positions in bytecode that are opcodes (not PUSH operands).
func findMutablePositions(code []byte) []int {
	var mutable []int
	i := 0
	for i < len(code) {
		opcode := code[i]
		mutable = append(mutable, i)

		if opcode >= 0x60 && opcode <= 0x7f {
			pushSize := int(opcode - 0x5f)
			i += pushSize + 1
		} else {
			i++
		}
	}
	return mutable
}

// hexToBytes parses a hex string (with optional 0x prefix) to bytes.
func hexToBytes(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s) == 0 {
		return nil, nil
	}
	if len(s)%2 != 0 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

// bytesToHex converts bytes to a hex string with 0x prefix.
func bytesToHex(b []byte) string {
	if len(b) == 0 {
		return "0x"
	}
	return "0x" + hex.EncodeToString(b)
}

// mutateCodeInTest is a helper that handles JSON parsing and applies a code mutation function.
func mutateCodeInTest(data []byte, rng *rand.Rand, mutateFunc func([]byte) []byte) ([]byte, string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", fmt.Errorf("failed to parse JSON: %w", err)
	}

	if len(raw) == 0 {
		return nil, "", ErrInvalidTest
	}

	var testName string
	var testData json.RawMessage
	for name, d := range raw {
		testName = name
		testData = d
		break
	}

	var test map[string]json.RawMessage
	if err := json.Unmarshal(testData, &test); err != nil {
		return nil, "", fmt.Errorf("failed to parse test: %w", err)
	}

	preData, ok := test["pre"]
	if !ok {
		return nil, "", ErrInvalidTest
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(preData, &pre); err != nil {
		return nil, "", fmt.Errorf("failed to parse 'pre': %w", err)
	}

	// Find accounts with code
	type accountWithCode struct {
		addr string
		code []byte
	}
	var candidates []accountWithCode

	for addr, accountData := range pre {
		var account map[string]json.RawMessage
		if err := json.Unmarshal(accountData, &account); err != nil {
			continue
		}
		codeData, ok := account["code"]
		if !ok {
			continue
		}
		var codeHex string
		if err := json.Unmarshal(codeData, &codeHex); err != nil {
			continue
		}
		code, err := hexToBytes(codeHex)
		if err != nil || len(code) == 0 {
			continue
		}
		candidates = append(candidates, accountWithCode{addr: addr, code: code})
	}

	if len(candidates) == 0 {
		return nil, "", ErrNoMutableTarget
	}

	target := candidates[rng.Intn(len(candidates))]
	mutatedCode := mutateFunc(target.code)

	var targetAccount map[string]json.RawMessage
	if err := json.Unmarshal(pre[target.addr], &targetAccount); err != nil {
		return nil, "", err
	}
	mutatedCodeHex, _ := json.Marshal(bytesToHex(mutatedCode))
	targetAccount["code"] = mutatedCodeHex

	updatedAccount, _ := json.Marshal(targetAccount)
	pre[target.addr] = updatedAccount

	updatedPre, _ := json.Marshal(pre)
	test["pre"] = updatedPre

	updatedTest, _ := json.Marshal(test)
	raw[testName] = updatedTest

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal result: %w", err)
	}

	return result, target.addr, nil
}
