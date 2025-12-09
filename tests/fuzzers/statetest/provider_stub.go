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

package statetest

import "testing"

// createGeneratorProvider creates a generator-only provider.
// This stub returns nil when built without the generators tag.
// Build with -tags=generators to enable generator support.
func createGeneratorProvider(t *testing.T, corpus *CoverageCorpus) InputProvider {
	return nil
}

// createHybridProvider creates a hybrid mutation+generation provider.
// This stub returns nil when built without the generators tag.
// Build with -tags=generators to enable generator support.
func createHybridProvider(t *testing.T, corpus *CoverageCorpus, strategy string) InputProvider {
	return nil
}
