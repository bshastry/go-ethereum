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
	"errors"

	"github.com/ethereum/go-ethereum/core/tracing"
)

// errExecutionCancelled is returned when execution is cancelled via context
var errExecutionCancelled = errors.New("execution cancelled")

// cancellationTracer checks for context cancellation during EVM execution.
// It hooks into OnOpcode to periodically check if the context has been cancelled,
// allowing for graceful termination of long-running or infinite-loop test cases.
type cancellationTracer struct {
	ctx     context.Context
	counter uint64
	// checkInterval controls how often we check for cancellation (every N opcodes)
	checkInterval uint64
}

// newCancellationTracer creates a new cancellation tracer
func newCancellationTracer(ctx context.Context) *cancellationTracer {
	return &cancellationTracer{
		ctx:           ctx,
		checkInterval: 100, // Check every 100 opcodes by default
	}
}

// newCancellationTracerWithInterval creates a tracer with custom check interval
func newCancellationTracerWithInterval(ctx context.Context, interval uint64) *cancellationTracer {
	if interval == 0 {
		interval = 100
	}
	return &cancellationTracer{
		ctx:           ctx,
		checkInterval: interval,
	}
}

// Hooks returns the tracing hooks for the cancellation tracer
func (t *cancellationTracer) Hooks() *tracing.Hooks {
	return &tracing.Hooks{
		OnOpcode: func(pc uint64, op byte, gas, cost uint64, scope tracing.OpContext, rData []byte, depth int, err error) {
			t.counter++
			// Only check for cancellation every N opcodes to minimize overhead
			if t.counter%t.checkInterval == 0 {
				if t.ctx.Err() != nil {
					// Context cancelled - panic to immediately stop execution
					// This will be caught by the caller's recover()
					panic(errExecutionCancelled)
				}
			}
		},
	}
}

// OpcodeCount returns the number of opcodes executed
func (t *cancellationTracer) OpcodeCount() uint64 {
	return t.counter
}

// Reset resets the opcode counter for reuse
func (t *cancellationTracer) Reset() {
	t.counter = 0
}

// isCancellationError returns true if the error is due to execution cancellation
func isCancellationError(err interface{}) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(error); ok {
		return errors.Is(e, errExecutionCancelled)
	}
	return err == errExecutionCancelled
}
