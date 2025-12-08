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
	"crypto/md5"
	"encoding/json"
	"hash"
	"strconv"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

// CanonicalOpLog represents a normalized EVM execution step.
// This format strips client-specific quirks for cross-client comparison.
// Matches goevmlab's CustomMarshal format with ClearGascost=true, ClearMemSize=true,
// ClearReturndata=true (the defaults used for cross-client comparison).
type CanonicalOpLog struct {
	Depth         int            `json:"depth"`
	Pc            uint64         `json:"pc"`
	Section       uint64         `json:"section,omitempty"`       // EOF only
	FunctionDepth int            `json:"functionDepth,omitempty"` // EOF only
	Gas           uint64         `json:"gas"`
	Op            byte           `json:"op"`
	OpName        string         `json:"opName"`
	Stack         []*uint256.Int `json:"stack"` // Last 6 items only
}

// lineCountingHasher wraps an MD5 hasher with line counting
type lineCountingHasher struct {
	h     hash.Hash
	lines int
}

func newLineCountingHasher() *lineCountingHasher {
	return &lineCountingHasher{
		h: md5.New(),
	}
}

func (lch *lineCountingHasher) Write(data []byte) (int, error) {
	return lch.h.Write(data)
}

func (lch *lineCountingHasher) WriteLine(data []byte) {
	lch.h.Write(data)
	lch.h.Write([]byte{'\n'})
	lch.lines++
}

func (lch *lineCountingHasher) Sum() []byte {
	return lch.h.Sum(nil)
}

func (lch *lineCountingHasher) Reset() {
	lch.h.Reset()
	lch.lines = 0
}

// TraceNormalizer normalizes EVM traces to a canonical format.
// It processes trace lines, filters out noise, and produces a
// deterministic hash for cross-client comparison.
type TraceNormalizer struct {
	hasher *lineCountingHasher
	prev   *CanonicalOpLog // For geth error line merging
}

// NewTraceNormalizer creates a new normalizer
func NewTraceNormalizer() *TraceNormalizer {
	return &TraceNormalizer{
		hasher: newLineCountingHasher(),
	}
}

// ProcessLine normalizes a single trace line (JSON format)
func (n *TraceNormalizer) ProcessLine(line []byte) error {
	var log CanonicalOpLog
	if err := json.Unmarshal(line, &log); err != nil {
		return nil // Skip unparseable lines
	}

	return n.ProcessLog(&log)
}

// ProcessLog normalizes a single trace log entry
func (n *TraceNormalizer) ProcessLog(log *CanonicalOpLog) error {
	// Filter: depth=0 means not a real opcode
	if log.Depth == 0 {
		return nil
	}

	// Filter: STOP opcodes at end (geth continues on virtual STOP)
	if log.Op == 0x00 { // STOP
		return nil
	}

	// Handle geth duplicate line merging (same PC+depth+functionDepth = merge)
	// Geth sometimes outputs two lines for the same opcode when there's an error
	if n.prev != nil {
		if n.prev.Pc == log.Pc && n.prev.Depth == log.Depth && n.prev.FunctionDepth == log.FunctionDepth {
			// Skip this line, it's a duplicate
			return nil
		}
		// Flush previous log
		n.writeNormalized(n.prev)
	}

	// Store current as previous for potential merging
	n.prev = log
	return nil
}

// writeNormalized writes a normalized log entry to the hasher
func (n *TraceNormalizer) writeNormalized(log *CanonicalOpLog) {
	data := canonicalMarshal(log)
	n.hasher.WriteLine(data)
}

// Finish flushes remaining data and returns the hash
func (n *TraceNormalizer) Finish() []byte {
	if n.prev != nil {
		n.writeNormalized(n.prev)
		n.prev = nil
	}
	return n.hasher.Sum()
}

// FinishWithStateRoot flushes remaining data, adds stateRoot as final line, and returns the hash.
// This matches goevmlab's behavior where stateRoot is included in the hash.
func (n *TraceNormalizer) FinishWithStateRoot(stateRoot string) []byte {
	if n.prev != nil {
		n.writeNormalized(n.prev)
		n.prev = nil
	}
	// Write stateRoot as final line (matches goevmlab format)
	if stateRoot != "" {
		stateRootJSON := `{"stateRoot":"` + stateRoot + `"}`
		n.hasher.WriteLine([]byte(stateRootJSON))
	}
	return n.hasher.Sum()
}

// Lines returns the number of trace lines processed
func (n *TraceNormalizer) Lines() int {
	return n.hasher.lines
}

// Reset resets the normalizer for reuse
func (n *TraceNormalizer) Reset() {
	n.hasher.Reset()
	n.prev = nil
}

// canonicalMarshal produces deterministic JSON output for an oplog.
// This mirrors goevmlab's CustomMarshal with default settings:
// ClearGascost=true, ClearMemSize=true, ClearReturndata=true
// Field order is fixed for deterministic hashing.
func canonicalMarshal(log *CanonicalOpLog) []byte {
	b := make([]byte, 0, 200)

	// Fixed field order for deterministic output (matches goevmlab)
	b = append(b, `{"depth":`...)
	b = strconv.AppendInt(b, int64(log.Depth), 10)

	b = append(b, `,"pc":`...)
	b = strconv.AppendUint(b, log.Pc, 10)

	// EOF-specific fields (omit if zero)
	if log.Section != 0 {
		b = append(b, `,"section":`...)
		b = strconv.AppendUint(b, log.Section, 10)
	}

	if log.FunctionDepth != 0 {
		b = append(b, `,"functionDepth":`...)
		b = strconv.AppendInt(b, int64(log.FunctionDepth), 10)
	}

	b = append(b, `,"gas":`...)
	b = strconv.AppendUint(b, log.Gas, 10)

	// Op as hex with 0x prefix (2 digits, zero-padded)
	b = append(b, `,"op":"0x`...)
	if log.Op < 0x10 {
		b = append(b, '0')
	}
	b = strconv.AppendUint(b, uint64(log.Op), 16)
	b = append(b, '"')

	b = append(b, `,"opName":"`...)
	b = append(b, vm.OpCode(log.Op).String()...)
	b = append(b, '"')

	// Stack: last 6 items only for deterministic comparison
	b = append(b, `,"stack":[`...)
	if len(log.Stack) > 0 {
		start := 0
		if len(log.Stack) > 6 {
			start = len(log.Stack) - 6
		}
		for i := start; i < len(log.Stack); i++ {
			if i != start {
				b = append(b, ',')
			}
			b = append(b, '"')
			if log.Stack[i] != nil {
				b = append(b, log.Stack[i].Hex()...)
			} else {
				b = append(b, "0x0"...)
			}
			b = append(b, '"')
		}
	}
	b = append(b, ']')

	b = append(b, '}')
	return b
}

// NormalizeTraceFromJSONL normalizes a JSONL trace (multiple lines)
func NormalizeTraceFromJSONL(jsonl []byte) (hash []byte, lines int, err error) {
	normalizer := NewTraceNormalizer()

	// Split by newlines and process each line
	start := 0
	for i := 0; i < len(jsonl); i++ {
		if jsonl[i] == '\n' {
			if i > start {
				if err := normalizer.ProcessLine(jsonl[start:i]); err != nil {
					return nil, 0, err
				}
			}
			start = i + 1
		}
	}
	// Process last line if not empty
	if start < len(jsonl) {
		if err := normalizer.ProcessLine(jsonl[start:]); err != nil {
			return nil, 0, err
		}
	}

	return normalizer.Finish(), normalizer.Lines(), nil
}
