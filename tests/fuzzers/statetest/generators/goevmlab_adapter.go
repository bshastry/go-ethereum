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

package generators

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/holiman/goevmlab/fuzzing"
)

// GoevmlabAdapter wraps a goevmlab factory function as a Generator.
// It provides thread-safe generation of state tests using goevmlab's
// semantic test generation capabilities.
type GoevmlabAdapter struct {
	name        string
	description string
	weight      int
	forks       []string
	gasLimit    uint64

	// Thread-safety: each adapter has its own mutex and RNG
	mu  sync.Mutex
	rng *rand.Rand
}

// NewGoevmlabAdapter creates an adapter for a goevmlab factory.
func NewGoevmlabAdapter(name, description string, weight int, forks []string, gasLimit uint64) *GoevmlabAdapter {
	return &GoevmlabAdapter{
		name:        name,
		description: description,
		weight:      weight,
		forks:       forks,
		gasLimit:    gasLimit,
		rng:         rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// Name returns the generator identifier.
func (g *GoevmlabAdapter) Name() string {
	return g.name
}

// Description returns a human-readable description.
func (g *GoevmlabAdapter) Description() string {
	return g.description
}

// Weight returns the relative selection weight.
func (g *GoevmlabAdapter) Weight() int {
	return g.weight
}

// SupportedForks returns the list of forks this generator supports.
func (g *GoevmlabAdapter) SupportedForks() []string {
	return g.forks
}

// Generate produces a new state test from the goevmlab factory.
// Thread-safe: uses internal mutex to protect the factory and RNG.
func (g *GoevmlabAdapter) Generate(fork string) ([]byte, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Get factory for this generator
	factory := fuzzing.Factory(g.name, fork, g.gasLimit)
	if factory == nil {
		return nil, fmt.Errorf("unknown goevmlab factory: %s", g.name)
	}

	// Generate the test
	gst := factory()

	// Fill to compute state root and logs hash
	if err := gst.Fill(nil); err != nil {
		return nil, fmt.Errorf("failed to fill state test: %w", err)
	}

	// Generate unique test name
	testName := fmt.Sprintf("generated_%s_%d", g.name, g.rng.Int63())

	// Convert to GeneralStateTest and marshal to JSON
	generalTest := gst.ToGeneralStateTest(testName)

	return json.Marshal(generalTest)
}

// Ensure GoevmlabAdapter implements Generator
var _ Generator = (*GoevmlabAdapter)(nil)
