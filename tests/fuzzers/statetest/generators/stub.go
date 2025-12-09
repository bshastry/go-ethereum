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

//go:build !generators

// Package generators provides stub implementations when built without the generators tag.
// Build with -tags=generators to enable goevmlab-based test generation.
package generators

// Registry is a stub when generators are not available.
type Registry struct{}

// NewRegistry returns nil when generators are not available.
// Build with -tags=generators to enable generator support.
func NewRegistry() *Registry {
	return nil
}

// Generator is the interface for test generators.
// This stub is provided for type checking when generators are disabled.
type Generator interface {
	Name() string
	Description() string
	Generate(fork string) ([]byte, error)
	Weight() int
	SupportedForks() []string
}

// GeneratorInfo contains metadata about a generator.
type GeneratorInfo struct {
	Name           string
	Description    string
	Weight         int
	SupportedForks []string
}

// GeneratorStats tracks statistics for a single generator.
type GeneratorStats struct {
	Name          string
	Generated     int64
	CoverageFinds int64
	TotalDelta    float64
}
