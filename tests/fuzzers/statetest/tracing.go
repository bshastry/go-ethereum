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

package statetest

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/tests"
	"github.com/holiman/uint256"
)

// TracingResult contains execution results with normalized trace hash
type TracingResult struct {
	StateRoot  string // Post-execution state root (hex)
	TraceHash  string // MD5 hash of normalized trace (hex)
	TraceLines int    // Number of trace lines
	GasUsed    uint64 // Gas consumed
}

// normalizingTracer captures and normalizes trace output
type normalizingTracer struct {
	ctx        context.Context
	normalizer *TraceNormalizer
	counter    uint64
	gasUsed    uint64

	// Check interval for cancellation
	checkInterval uint64
}

// newNormalizingTracer creates a new normalizing tracer
func newNormalizingTracer(ctx context.Context) *normalizingTracer {
	return &normalizingTracer{
		ctx:           ctx,
		normalizer:    NewTraceNormalizer(),
		checkInterval: 100,
	}
}

// Hooks returns the tracing hooks for the normalizing tracer
func (t *normalizingTracer) Hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			// Check cancellation periodically
			t.counter++
			if t.counter%t.checkInterval == 0 {
				if t.ctx.Err() != nil {
					panic(errExecutionCancelled)
				}
			}

			// Track gas usage
			t.gasUsed += cost

			// Build canonical log entry
			log := &CanonicalOpLog{
				Depth:      depth,
				Pc:         pc,
				Gas:        gas,
				Op:         op,
				OpName:     vm.OpCode(op).String(),
				GasCost:    cost,
				MemorySize: len(scope.MemoryData()),
			}

			// Capture stack (last 6 items for determinism)
			stackData := scope.StackData()
			if len(stackData) > 0 {
				start := 0
				if len(stackData) > 6 {
					start = len(stackData) - 6
				}
				log.Stack = make([]*uint256.Int, len(stackData)-start)
				for i := start; i < len(stackData); i++ {
					val := new(uint256.Int).Set(&stackData[i])
					log.Stack[i-start] = val
				}
			}

			// Capture return data if present
			if len(rData) > 0 {
				log.ReturnData = make([]byte, len(rData))
				copy(log.ReturnData, rData)
			}

			// Capture error
			if err != nil {
				log.Error = err.Error()
			}

			// Feed to normalizer
			t.normalizer.ProcessLog(log)
		},
	}
}

// Finish completes tracing and returns the normalized hash
func (t *normalizingTracer) Finish() (hash []byte, lines int) {
	return t.normalizer.Finish(), t.normalizer.Lines()
}

// GasUsed returns the total gas consumed
func (t *normalizingTracer) GasUsed() uint64 {
	return t.gasUsed
}

// executeWithTracing runs a state test and returns normalized trace hash
func executeWithTracing(
	ctx context.Context,
	testJSON []byte,
	timeout time.Duration,
) (*TracingResult, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stateTests map[string]tests.StateTest
	if err := json.Unmarshal(testJSON, &stateTests); err != nil {
		return nil, err
	}

	tracer := newNormalizingTracer(ctx)

	var result TracingResult

	// Execute with panic recovery
	func() {
		defer func() {
			if r := recover(); r != nil {
				if !isCancellationError(r) {
					// Re-panic for real errors
					panic(r)
				}
			}
		}()

		for _, test := range stateTests {
			for _, subtest := range test.Subtests() {
				if !isSupportedFork(subtest.Fork) {
					continue
				}

				st, root, _, err := test.RunNoVerify(
					subtest,
					vm.Config{Tracer: tracer.Hooks()},
					false,
					rawdb.HashScheme,
				)
				if err == nil {
					result.StateRoot = root.Hex()
				}

				if st.StateDB != nil {
					st.Close()
				}
			}
		}
	}()

	// Finalize and get trace hash
	hashBytes, lines := tracer.Finish()
	result.TraceHash = hex.EncodeToString(hashBytes)
	result.TraceLines = lines
	result.GasUsed = tracer.GasUsed()

	return &result, nil
}

// ExecuteAndNormalize is the public API for executing with trace normalization
func ExecuteAndNormalize(testJSON []byte, timeout time.Duration) (*TracingResult, error) {
	return executeWithTracing(context.Background(), testJSON, timeout)
}
