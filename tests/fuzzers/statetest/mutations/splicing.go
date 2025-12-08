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
	"bytes"
	"encoding/json"
	"math/rand"
)

// SplicingStrategy combines bytecode from different corpus inputs (AFL splicing stage).
// AFL's splicing finds a point where two inputs differ and creates a hybrid by
// combining the prefix of one with the suffix of another.
type SplicingStrategy struct {
	corpus CorpusProvider
	rng    *rand.Rand
}

// NewSplicingStrategy creates a splicing strategy with corpus access.
// If corpus is nil, the strategy will return ErrNoMutableTarget.
func NewSplicingStrategy(corpus CorpusProvider) *SplicingStrategy {
	return &SplicingStrategy{
		corpus: corpus,
		rng:    rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *SplicingStrategy) Name() string { return "splicing" }

// Description returns a brief description of this strategy.
func (s *SplicingStrategy) Description() string {
	return "AFL splicing: combine bytecode from different corpus inputs"
}

// Weight returns the relative weight for this strategy.
// Lower weight (4) since it requires corpus access and is more expensive.
func (s *SplicingStrategy) Weight() int { return 4 }

// Mutate combines bytecode from the input with bytecode from another corpus entry.
func (s *SplicingStrategy) Mutate(data []byte) ([]byte, string, error) {
	// Check corpus availability
	if s.corpus == nil || s.corpus.GetInputCount() < 2 {
		return nil, "", ErrNoMutableTarget
	}

	// Get another random input from corpus
	other, err := s.corpus.GetRandomInput()
	if err != nil {
		return nil, "", ErrNoMutableTarget
	}

	// Extract bytecode from both inputs
	currentCode, currentAddr, err := s.extractBytecode(data)
	if err != nil || len(currentCode) < 4 {
		return nil, "", ErrNoMutableTarget
	}

	otherCode, _, err := s.extractBytecode(other)
	if err != nil || len(otherCode) < 4 {
		return nil, "", ErrNoMutableTarget
	}

	// Find splice point and combine
	spliced := s.spliceBytecode(currentCode, otherCode)
	if spliced == nil || (len(spliced) == len(currentCode) && bytes.Equal(spliced, currentCode)) {
		return nil, "", ErrNoMutableTarget
	}

	// Replace bytecode in original test
	return s.replaceBytecode(data, currentAddr, spliced)
}

// extractBytecode finds the first account with code in pre-state.
func (s *SplicingStrategy) extractBytecode(data []byte) ([]byte, string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", err
	}

	// Get test data (first test in the file)
	var testData json.RawMessage
	for _, td := range raw {
		testData = td
		break
	}

	var test map[string]json.RawMessage
	if err := json.Unmarshal(testData, &test); err != nil {
		return nil, "", err
	}

	// Parse pre section
	preRaw, ok := test["pre"]
	if !ok {
		return nil, "", ErrNoMutableTarget
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(preRaw, &pre); err != nil {
		return nil, "", err
	}

	// Find first account with non-empty code
	for addr, accountData := range pre {
		var account map[string]json.RawMessage
		if err := json.Unmarshal(accountData, &account); err != nil {
			continue
		}

		codeRaw, ok := account["code"]
		if !ok {
			continue
		}

		var codeHex string
		if err := json.Unmarshal(codeRaw, &codeHex); err != nil {
			continue
		}

		code, err := hexToBytes(codeHex)
		if err != nil || len(code) == 0 {
			continue
		}

		return code, addr, nil
	}

	return nil, "", ErrNoMutableTarget
}

// spliceBytecode combines two bytecode sequences at a differing point.
// This implements AFL's splice algorithm: find first and last differing positions,
// pick a random point between them, and create a hybrid.
func (s *SplicingStrategy) spliceBytecode(a, b []byte) []byte {
	if len(a) < 4 || len(b) < 4 {
		return nil
	}

	// Find first and last differing positions
	minLen := len(a)
	if len(b) < minLen {
		minLen = len(b)
	}

	firstDiff, lastDiff := -1, -1
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			if firstDiff == -1 {
				firstDiff = i
			}
			lastDiff = i
		}
	}

	// Need at least 2 bytes between first and last diff for meaningful splice
	if firstDiff < 0 || lastDiff-firstDiff < 2 {
		return nil
	}

	// Pick splice point between first and last diff
	spliceAt := firstDiff + s.rng.Intn(lastDiff-firstDiff)

	// Create hybrid: [a: 0..spliceAt] + [b: spliceAt..end]
	result := make([]byte, spliceAt+len(b)-spliceAt)
	copy(result[:spliceAt], a[:spliceAt])
	copy(result[spliceAt:], b[spliceAt:])

	return result
}

// replaceBytecode replaces bytecode at the given address in the test data.
func (s *SplicingStrategy) replaceBytecode(data []byte, addr string, newCode []byte) ([]byte, string, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, "", err
	}

	var testName string
	var testData json.RawMessage
	for name, td := range raw {
		testName = name
		testData = td
		break
	}

	var test map[string]json.RawMessage
	if err := json.Unmarshal(testData, &test); err != nil {
		return nil, "", err
	}

	var pre map[string]json.RawMessage
	if err := json.Unmarshal(test["pre"], &pre); err != nil {
		return nil, "", err
	}

	var account map[string]json.RawMessage
	if err := json.Unmarshal(pre[addr], &account); err != nil {
		return nil, "", err
	}

	// Replace code
	codeHex := bytesToHex(newCode)
	codeBytes, _ := json.Marshal(codeHex)
	account["code"] = codeBytes

	// Rebuild JSON tree
	accountBytes, _ := json.Marshal(account)
	pre[addr] = accountBytes

	preBytes, _ := json.Marshal(pre)
	test["pre"] = preBytes

	testBytes, _ := json.Marshal(test)
	raw[testName] = testBytes

	result, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return nil, "", err
	}

	return result, "spliced:" + addr, nil
}
