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

//go:build generators

// Package generators provides test generation capabilities for the statetest fuzzer.
// This package wraps goevmlab's factory-based test generators to produce semantically
// rich state tests that target specific EVM features like precompiles, opcodes, and
// gas metering edge cases.
//
// Build with -tags=generators to enable this package.
package generators

// Generator produces fresh state test JSON from semantic knowledge.
// Unlike MutationStrategy, generators don't require an input to transform.
// Each generator specializes in a particular aspect of EVM behavior.
//
// Implementations must be thread-safe for concurrent access.
type Generator interface {
	// Name returns the generator identifier (e.g., "ecrecover", "bls", "modexp").
	Name() string

	// Description returns a human-readable description of what this generator tests.
	Description() string

	// Generate produces a new state test as JSON bytes.
	// Each call should produce a different test (uses internal RNG).
	// The fork parameter specifies the target fork (e.g., "Cancun", "Prague", "Osaka").
	Generate(fork string) ([]byte, error)

	// Weight returns the relative selection weight for combined generation.
	// Higher weights mean more frequent selection in weighted random selection.
	Weight() int

	// SupportedForks returns the list of forks this generator supports.
	// Empty slice means all forks are supported.
	SupportedForks() []string
}

// GeneratorInfo contains metadata about a generator.
type GeneratorInfo struct {
	Name           string   // Generator identifier
	Description    string   // Human-readable description
	Weight         int      // Selection weight
	SupportedForks []string // Forks this generator supports (nil = all)
}

// ForkSupported returns true if the generator supports the given fork.
func (g *GeneratorInfo) ForkSupported(fork string) bool {
	if len(g.SupportedForks) == 0 {
		return true // Empty means all forks
	}
	for _, f := range g.SupportedForks {
		if f == fork {
			return true
		}
	}
	return false
}

// GeneratorStats tracks statistics for a single generator.
type GeneratorStats struct {
	Name          string  // Generator name
	Generated     int64   // Total tests generated
	CoverageFinds int64   // Tests that found coverage
	TotalDelta    float64 // Sum of coverage deltas
}

// FindRate returns the ratio of coverage finds to total generated.
func (s *GeneratorStats) FindRate() float64 {
	if s.Generated == 0 {
		return 0
	}
	return float64(s.CoverageFinds) / float64(s.Generated)
}

// AvgDelta returns the average coverage delta per find.
func (s *GeneratorStats) AvgDelta() float64 {
	if s.CoverageFinds == 0 {
		return 0
	}
	return s.TotalDelta / float64(s.CoverageFinds)
}

// Clone returns a copy of the stats.
func (s *GeneratorStats) Clone() *GeneratorStats {
	return &GeneratorStats{
		Name:          s.Name,
		Generated:     s.Generated,
		CoverageFinds: s.CoverageFinds,
		TotalDelta:    s.TotalDelta,
	}
}
