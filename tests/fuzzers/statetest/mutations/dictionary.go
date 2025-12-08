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
	"math/rand"
)

// DictionaryStrategy injects EVM-specific tokens into bytecode.
// This is inspired by AFL's dictionary mode which maintains a list of
// domain-specific tokens that are likely to trigger interesting behavior.
type DictionaryStrategy struct {
	rng        *rand.Rand
	dictionary [][]byte
}

// NewDictionaryStrategy creates a new dictionary injection strategy.
func NewDictionaryStrategy() *DictionaryStrategy {
	// Flatten dictionary into slice for easier random selection
	dict := make([][]byte, 0, len(evmDictionary))
	for _, v := range evmDictionary {
		dict = append(dict, v)
	}
	return &DictionaryStrategy{
		rng:        rand.New(rand.NewSource(rand.Int63())),
		dictionary: dict,
	}
}

// Name returns the strategy name.
func (s *DictionaryStrategy) Name() string { return "dictionary" }

// Description returns a brief description of this strategy.
func (s *DictionaryStrategy) Description() string {
	return "Inject EVM-specific dictionary tokens into bytecode"
}

// Weight returns the relative weight for this strategy.
func (s *DictionaryStrategy) Weight() int { return 8 }

// evmDictionary contains EVM-specific byte sequences that are likely to
// trigger interesting behavior when injected into bytecode.
var evmDictionary = map[string][]byte{
	// Critical state-changing opcodes
	"STOP":         {0x00},
	"CALL":         {0xF1},
	"CALLCODE":     {0xF2},
	"RETURN":       {0xF3},
	"DELEGATECALL": {0xF4},
	"CREATE2":      {0xF5},
	"STATICCALL":   {0xFA},
	"REVERT":       {0xFD},
	"INVALID":      {0xFE},
	"SELFDESTRUCT": {0xFF},
	"CREATE":       {0xF0},

	// Storage operations
	"SLOAD":  {0x54},
	"SSTORE": {0x55},
	"TLOAD":  {0x5C}, // EIP-1153
	"TSTORE": {0x5D}, // EIP-1153

	// Memory operations
	"MLOAD":  {0x51},
	"MSTORE": {0x52},
	"MCOPY":  {0x5E}, // EIP-5656

	// Common PUSH sequences
	"PUSH1_0":        {0x60, 0x00},
	"PUSH1_1":        {0x60, 0x01},
	"PUSH1_FF":       {0x60, 0xFF},
	"PUSH1_20":       {0x60, 0x20}, // 32 bytes
	"PUSH2_FFFF":     {0x61, 0xFF, 0xFF},
	"PUSH4_FFFFFFFF": {0x63, 0xFF, 0xFF, 0xFF, 0xFF},

	// Jump operations
	"JUMP":     {0x56},
	"JUMPI":    {0x57},
	"JUMPDEST": {0x5B},

	// Control flow patterns
	"RETURN_EMPTY": {0x60, 0x00, 0x60, 0x00, 0xF3}, // PUSH1 0 PUSH1 0 RETURN
	"REVERT_EMPTY": {0x60, 0x00, 0x60, 0x00, 0xFD}, // PUSH1 0 PUSH1 0 REVERT
	"STOP_ALONE":   {0x00},

	// Block info opcodes
	"BLOBHASH":    {0x49}, // EIP-4844
	"BLOBBASEFEE": {0x4A}, // EIP-7516
	"BLOCKHASH":   {0x40},
	"COINBASE":    {0x41},
	"TIMESTAMP":   {0x42},
	"NUMBER":      {0x43},
	"PREVRANDAO":  {0x44},
	"GASLIMIT":    {0x45},
	"CHAINID":     {0x46},
	"BASEFEE":     {0x48},

	// Precompile call patterns (CALL to precompile addresses)
	"CALL_ECRECOVER": {0x60, 0x01}, // PUSH1 1 (ecrecover address)
	"CALL_SHA256":    {0x60, 0x02}, // PUSH1 2
	"CALL_RIPEMD":    {0x60, 0x03}, // PUSH1 3
	"CALL_IDENTITY":  {0x60, 0x04}, // PUSH1 4
	"CALL_MODEXP":    {0x60, 0x05}, // PUSH1 5
	"CALL_BN254ADD":  {0x60, 0x06}, // PUSH1 6
	"CALL_BN254MUL":  {0x60, 0x07}, // PUSH1 7
	"CALL_BN254PAIR": {0x60, 0x08}, // PUSH1 8
	"CALL_BLAKE2F":   {0x60, 0x09}, // PUSH1 9
	"CALL_KZG":       {0x60, 0x0A}, // PUSH1 10 (KZG point eval)

	// Common function selectors (for calldata injection)
	"TRANSFER_SELECTOR":    {0xa9, 0x05, 0x9c, 0xbb}, // transfer(address,uint256)
	"APPROVE_SELECTOR":     {0x09, 0x5e, 0xa7, 0xb3}, // approve(address,uint256)
	"BALANCEOF_SELECTOR":   {0x70, 0xa0, 0x82, 0x31}, // balanceOf(address)
	"TOTALSUPPLY_SELECTOR": {0x18, 0x16, 0x0d, 0xdd}, // totalSupply()

	// Gas-related opcodes
	"GAS":         {0x5A},
	"GASPRICE":    {0x3A},
	"SELFBALANCE": {0x47},

	// Code/data copy opcodes
	"CODECOPY":       {0x39},
	"CALLDATACOPY":   {0x37},
	"EXTCODECOPY":    {0x3C},
	"RETURNDATACOPY": {0x3E},
}

// PUSH32 max uint256 - initialized separately to avoid compile-time issue.
func init() {
	maxUint := make([]byte, 33)
	maxUint[0] = 0x7F // PUSH32
	for i := 1; i < 33; i++ {
		maxUint[i] = 0xFF
	}
	evmDictionary["PUSH32_MAXUINT"] = maxUint
}

// Mutate performs mutation on raw JSON test data.
func (s *DictionaryStrategy) Mutate(data []byte) ([]byte, string, error) {
	// 50% overwrite at random position, 50% insert
	if s.rng.Intn(2) == 0 {
		return s.overwriteToken(data)
	}
	return s.insertToken(data)
}

func (s *DictionaryStrategy) overwriteToken(data []byte) ([]byte, string, error) {
	return mutateCodeInTest(data, s.rng, func(code []byte) []byte {
		if len(code) == 0 || len(s.dictionary) == 0 {
			return code
		}

		// Select random token
		token := s.dictionary[s.rng.Intn(len(s.dictionary))]

		// Select position to overwrite
		if len(code) < len(token) {
			// Code too short, just append
			return append(code, token...)
		}

		pos := s.rng.Intn(len(code) - len(token) + 1)

		// Create mutated copy
		mutated := make([]byte, len(code))
		copy(mutated, code)
		copy(mutated[pos:], token)

		return mutated
	})
}

func (s *DictionaryStrategy) insertToken(data []byte) ([]byte, string, error) {
	return mutateCodeInTest(data, s.rng, func(code []byte) []byte {
		if len(s.dictionary) == 0 {
			return code
		}

		// Select random token
		token := s.dictionary[s.rng.Intn(len(s.dictionary))]

		// Select insertion position
		pos := 0
		if len(code) > 0 {
			pos = s.rng.Intn(len(code) + 1)
		}

		// Create mutated copy with insertion
		mutated := make([]byte, 0, len(code)+len(token))
		mutated = append(mutated, code[:pos]...)
		mutated = append(mutated, token...)
		mutated = append(mutated, code[pos:]...)

		return mutated
	})
}
