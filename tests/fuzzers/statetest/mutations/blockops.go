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

// BlockOpsStrategy performs AFL-style block-level mutations on bytecode.
// AFL uses these extensively in its deterministic and havoc stages for
// operations like delete, clone, insert, and overwrite.
type BlockOpsStrategy struct {
	rng *rand.Rand
}

// NewBlockOpsStrategy creates a new block operations mutation strategy.
func NewBlockOpsStrategy() *BlockOpsStrategy {
	return &BlockOpsStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *BlockOpsStrategy) Name() string { return "blockops" }

// Description returns a brief description of this strategy.
func (s *BlockOpsStrategy) Description() string {
	return "AFL-style block operations (delete, clone, insert, overwrite)"
}

// Weight returns the relative weight for this strategy.
func (s *BlockOpsStrategy) Weight() int { return 7 }

// chooseBlockLen selects block size using AFL's adaptive sizing.
// AFL uses different max sizes based on fuzzing cycle.
func chooseBlockLen(limit int, rng *rand.Rand) int {
	// Simplified from AFL's cycle-based selection
	maxSizes := []int{32, 128, 1500, 32768}
	maxSize := maxSizes[rng.Intn(len(maxSizes))]
	if maxSize > limit {
		maxSize = limit
	}
	if maxSize < 1 {
		return 1
	}
	return 1 + rng.Intn(maxSize)
}

// Mutate performs mutation on raw JSON test data.
func (s *BlockOpsStrategy) Mutate(data []byte) ([]byte, string, error) {
	// Select operation: weight delete 2x to prevent input bloat
	// 0=delete, 1=clone, 2=insert, 3=overwrite
	ops := []int{0, 0, 1, 2, 3}
	op := ops[s.rng.Intn(len(ops))]

	var opName string
	switch op {
	case 0:
		opName = "delete"
	case 1:
		opName = "clone"
	case 2:
		opName = "insert"
	case 3:
		opName = "overwrite"
	}

	result, addr, err := mutateCodeInTest(data, s.rng, func(code []byte) []byte {
		switch op {
		case 0:
			return s.deleteBlock(code)
		case 1:
			return s.cloneBlock(code)
		case 2:
			return s.insertBlock(code)
		case 3:
			return s.overwriteBlock(code)
		}
		return code
	})
	if err != nil {
		return nil, "", err
	}
	return result, opName + ":" + addr, nil
}

// deleteBlock removes a block of bytes from the data.
func (s *BlockOpsStrategy) deleteBlock(data []byte) []byte {
	if len(data) < 4 {
		return data
	}
	delLen := chooseBlockLen(len(data)-1, s.rng)
	if delLen >= len(data) {
		delLen = len(data) - 1
	}
	delFrom := s.rng.Intn(len(data) - delLen)
	return append(data[:delFrom], data[delFrom+delLen:]...)
}

// cloneBlock copies a block of bytes to another position.
func (s *BlockOpsStrategy) cloneBlock(data []byte) []byte {
	if len(data) < 2 {
		return data
	}
	cloneLen := chooseBlockLen(len(data), s.rng)
	if cloneLen > len(data) {
		cloneLen = len(data)
	}
	from := s.rng.Intn(len(data) - cloneLen + 1)
	to := s.rng.Intn(len(data))

	result := make([]byte, 0, len(data)+cloneLen)
	result = append(result, data[:to]...)
	result = append(result, data[from:from+cloneLen]...)
	result = append(result, data[to:]...)
	return result
}

// insertBlock inserts random bytes at a position.
func (s *BlockOpsStrategy) insertBlock(data []byte) []byte {
	insertLen := chooseBlockLen(256, s.rng) // Max insert 256 bytes
	pos := s.rng.Intn(len(data) + 1)

	insert := make([]byte, insertLen)
	s.rng.Read(insert)

	result := make([]byte, 0, len(data)+insertLen)
	result = append(result, data[:pos]...)
	result = append(result, insert...)
	result = append(result, data[pos:]...)
	return result
}

// overwriteBlock overwrites a block with constant or random bytes.
func (s *BlockOpsStrategy) overwriteBlock(data []byte) []byte {
	if len(data) < 2 {
		return data
	}
	overLen := chooseBlockLen(len(data), s.rng)
	if overLen > len(data) {
		overLen = len(data)
	}
	pos := s.rng.Intn(len(data) - overLen + 1)

	result := make([]byte, len(data))
	copy(result, data)

	// 50% constant fill, 50% random
	if s.rng.Intn(2) == 0 {
		fill := byte(s.rng.Intn(256))
		for i := 0; i < overLen; i++ {
			result[pos+i] = fill
		}
	} else {
		s.rng.Read(result[pos : pos+overLen])
	}
	return result
}
