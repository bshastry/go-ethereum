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
	"errors"
	"sync"
)

// ErrUnknownGenerator is returned when requesting an unknown generator.
var ErrUnknownGenerator = errors.New("unknown generator")

// Registry manages available generators with thread-safe access.
// It provides factory functions for creating generator instances.
type Registry struct {
	mu           sync.RWMutex
	constructors map[string]func() Generator
	info         map[string]*GeneratorInfo
}

// NewRegistry creates a registry with default goevmlab generators.
func NewRegistry() *Registry {
	r := &Registry{
		constructors: make(map[string]func() Generator),
		info:         make(map[string]*GeneratorInfo),
	}
	r.registerDefaults()
	return r
}

// Register adds a generator constructor to the registry.
// The constructor is called each time a new generator instance is needed.
func (r *Registry) Register(name string, constructor func() Generator, info *GeneratorInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.constructors[name] = constructor
	r.info[name] = info
}

// Get returns a new instance of the named generator.
// Returns ErrUnknownGenerator if the name is not registered.
func (r *Registry) Get(name string) (Generator, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	constructor, ok := r.constructors[name]
	if !ok {
		return nil, ErrUnknownGenerator
	}
	return constructor(), nil
}

// All returns new instances of all registered generators.
func (r *Registry) All() []Generator {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Generator, 0, len(r.constructors))
	for _, constructor := range r.constructors {
		result = append(result, constructor())
	}
	return result
}

// Names returns all registered generator names.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.constructors))
	for name := range r.constructors {
		names = append(names, name)
	}
	return names
}

// Info returns metadata about the named generator.
func (r *Registry) Info(name string) (*GeneratorInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	info, ok := r.info[name]
	if !ok {
		return nil, ErrUnknownGenerator
	}
	return info, nil
}

// AllInfo returns metadata for all registered generators.
func (r *Registry) AllInfo() []*GeneratorInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*GeneratorInfo, 0, len(r.info))
	for _, info := range r.info {
		result = append(result, info)
	}
	return result
}

// ForFork returns new instances of generators that support the given fork.
func (r *Registry) ForFork(fork string) []Generator {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Generator, 0, len(r.constructors))
	for name, constructor := range r.constructors {
		info := r.info[name]
		if info != nil && info.ForkSupported(fork) {
			result = append(result, constructor())
		}
	}
	return result
}

// NamesForFork returns names of generators that support the given fork.
func (r *Registry) NamesForFork(fork string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.constructors))
	for name := range r.constructors {
		info := r.info[name]
		if info != nil && info.ForkSupported(fork) {
			names = append(names, name)
		}
	}
	return names
}

// Count returns the number of registered generators.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.constructors)
}

// registerDefaults registers all goevmlab factory generators.
// This is called by NewRegistry to populate the default generators.
func (r *Registry) registerDefaults() {
	// Register all goevmlab factories
	// Each entry: name, description, weight, supported forks (nil = all)

	type genDef struct {
		name  string
		desc  string
		weight int
		forks  []string
	}

	defs := []genDef{
		{
			name:   "ecrecover",
			desc:   "ECRECOVER precompile (0x01) - ECDSA signature recovery edge cases",
			weight: 10,
			forks:  nil, // All forks
		},
		{
			name:   "naive",
			desc:   "Random bytecode generation - pure instruction coverage",
			weight: 5,
			forks:  nil,
		},
		{
			name:   "blake",
			desc:   "BLAKE2F precompile (0x09, EIP-152) - cryptographic hash compression",
			weight: 8,
			forks:  []string{"Istanbul", "Berlin", "London", "Paris", "Shanghai", "Cancun", "Prague", "Osaka"},
		},
		{
			name:   "bls",
			desc:   "BLS12-381 precompiles (0x0b-0x11, EIP-2537) - elliptic curve operations",
			weight: 12,
			forks:  []string{"Prague", "Osaka"},
		},
		{
			name:   "bn254",
			desc:   "BN254 elliptic curve precompiles (0x06-0x08) - pairing and scalar ops",
			weight: 10,
			forks:  nil,
		},
		{
			name:   "precompiles",
			desc:   "Mixed precompile calls - fork-aware comprehensive precompile testing",
			weight: 15,
			forks:  nil,
		},
		{
			name:   "simpleops",
			desc:   "Arithmetic and logic operations (ADD, MUL, DIV, etc.)",
			weight: 8,
			forks:  nil,
		},
		{
			name:   "memops",
			desc:   "Memory-interacting operations (MLOAD, MSTORE, MCOPY, etc.)",
			weight: 10,
			forks:  nil,
		},
		{
			name:   "sstore_sload",
			desc:   "Storage operations (EIP-2200) - SSTORE/SLOAD gas metering",
			weight: 12,
			forks:  nil,
		},
		{
			name:   "tstore_tload",
			desc:   "Transient storage (EIP-1153) - block-scoped TSTORE/TLOAD",
			weight: 10,
			forks:  []string{"Cancun", "Prague", "Osaka"},
		},
		{
			name:   "auth",
			desc:   "EIP-7702 account abstraction - SET_CODE transactions",
			weight: 15,
			forks:  []string{"Prague", "Osaka"},
		},
		{
			name:   "kzg",
			desc:   "KZG point evaluation (0x0a, EIP-4844) - blob commitments",
			weight: 12,
			forks:  []string{"Cancun", "Prague", "Osaka"},
		},
		{
			name:   "p256",
			desc:   "P256VERIFY precompile (0x100, EIP-7212) - secp256r1 signature verification",
			weight: 12,
			forks:  []string{"Prague", "Osaka"},
		},
		{
			name:   "modexp",
			desc:   "MODEXP precompile (0x05) - modular exponentiation with EIP-7823 pricing",
			weight: 10,
			forks:  nil,
		},
	}

	for _, def := range defs {
		defCopy := def // Capture for closure
		info := &GeneratorInfo{
			Name:           defCopy.name,
			Description:    defCopy.desc,
			Weight:         defCopy.weight,
			SupportedForks: defCopy.forks,
		}
		r.Register(defCopy.name, func() Generator {
			return NewGoevmlabAdapter(defCopy.name, defCopy.desc, defCopy.weight, defCopy.forks, 16_000_000)
		}, info)
	}
}
