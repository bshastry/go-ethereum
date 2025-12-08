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

// BitFlipStrategy performs AFL-style systematic bit flipping mutations
// on bytecode. AFL uses FLIP1/FLIP2/FLIP4/FLIP8 stages that flip
// consecutive bits at each position.
type BitFlipStrategy struct {
	rng *rand.Rand
}

// NewBitFlipStrategy creates a new bit flip mutation strategy.
func NewBitFlipStrategy() *BitFlipStrategy {
	return &BitFlipStrategy{
		rng: rand.New(rand.NewSource(rand.Int63())),
	}
}

// Name returns the strategy name.
func (s *BitFlipStrategy) Name() string { return "bitflip" }

// Description returns a brief description of this strategy.
func (s *BitFlipStrategy) Description() string {
	return "AFL-style bit flipping (1/2/4/8 consecutive bits)"
}

// Weight returns the relative weight for this strategy.
func (s *BitFlipStrategy) Weight() int { return 6 }

// flipSizes represents AFL's FLIP1, FLIP2, FLIP4, FLIP8 stages.
var flipSizes = []int{1, 2, 4, 8}

// Mutate performs mutation on raw JSON test data.
func (s *BitFlipStrategy) Mutate(data []byte) ([]byte, string, error) {
	// Select flip size: 1, 2, 4, or 8 bits
	flipSize := flipSizes[s.rng.Intn(len(flipSizes))]

	// Apply to bytecode using mutateCodeInTest helper
	return mutateCodeInTest(data, s.rng, func(code []byte) []byte {
		return s.flipBits(code, flipSize)
	})
}

// flipBits flips numBits consecutive bits starting at a random position.
func (s *BitFlipStrategy) flipBits(data []byte, numBits int) []byte {
	if len(data) == 0 {
		return data
	}

	// Calculate total bits available
	totalBits := len(data) * 8

	// Need at least numBits to flip
	if totalBits < numBits {
		return data
	}

	// Select random starting bit position
	startBit := s.rng.Intn(totalBits - numBits + 1)

	// Create mutated copy
	result := make([]byte, len(data))
	copy(result, data)

	// Flip consecutive bits
	for i := 0; i < numBits; i++ {
		bitPos := startBit + i
		bytePos := bitPos / 8
		bitOffset := uint(bitPos % 8)
		// Flip bit (MSB first ordering, like AFL)
		result[bytePos] ^= (1 << (7 - bitOffset))
	}

	return result
}
