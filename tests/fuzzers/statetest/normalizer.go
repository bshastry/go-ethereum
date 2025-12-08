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
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
)

// FilterReason describes why a trace line was filtered out
type FilterReason string

const (
	FilterReasonStopOpcode FilterReason = "STOP_OPCODE"
	FilterReasonDepthZero  FilterReason = "DEPTH_ZERO"
	FilterReasonDuplicate  FilterReason = "DUPLICATE"
)

// TraceDumpMeta contains metadata for trace dump files (JSONL header)
type TraceDumpMeta struct {
	Client            string `json:"client"`
	Version           string `json:"version"`
	Fork              string `json:"fork"`
	InputHash         string `json:"inputHash"`
	TestName          string `json:"testName"`
	Timestamp         string `json:"timestamp"`
	NormalizerVersion string `json:"normalizerVersion"`
}

// TraceDumpFiltered represents a filtered trace entry
type TraceDumpFiltered struct {
	Reason        FilterReason `json:"reason"`
	Depth         int          `json:"depth"`
	Pc            uint64       `json:"pc"`
	Op            string       `json:"op"`
	OpName        string       `json:"opName"`
	FunctionDepth int          `json:"functionDepth,omitempty"`
}

// TraceDumpResult contains the final result summary for trace dump
type TraceDumpResult struct {
	TraceHash     string `json:"traceHash"`
	TraceLines    int    `json:"traceLines"`
	FinalLineHash string `json:"finalLineHash"`
}

// DumpTraceWriter is an interface for writing trace lines during normalization.
// This is used for debugging cross-VM divergences by dumping normalized traces.
type DumpTraceWriter interface {
	// WriteMeta writes the metadata header (first line of JSONL output)
	WriteMeta(meta *TraceDumpMeta) error
	// WriteTraceLine writes a normalized trace line
	WriteTraceLine(log *CanonicalOpLog) error
	// WriteFiltered writes a filtered entry (when --dump-filtered is enabled)
	WriteFiltered(reason FilterReason, log *CanonicalOpLog) error
	// WriteStateRoot writes the stateRoot line
	WriteStateRoot(stateRoot string) error
	// WriteResult writes the final result summary
	WriteResult(result *TraceDumpResult) error
	// Close closes the writer
	Close() error
}

// FileDumpTraceWriter writes trace dump to a file in JSONL format
type FileDumpTraceWriter struct {
	file         *os.File
	includeFiltered bool
}

// NewFileDumpTraceWriter creates a new file-based trace dump writer
func NewFileDumpTraceWriter(path string, includeFiltered bool) (*FileDumpTraceWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &FileDumpTraceWriter{
		file:            f,
		includeFiltered: includeFiltered,
	}, nil
}

func (w *FileDumpTraceWriter) writeLine(data []byte) error {
	_, err := w.file.Write(data)
	if err != nil {
		return err
	}
	_, err = w.file.Write([]byte{'\n'})
	return err
}

// WriteMeta writes the metadata header
func (w *FileDumpTraceWriter) WriteMeta(meta *TraceDumpMeta) error {
	wrapper := map[string]*TraceDumpMeta{"_meta": meta}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// WriteTraceLine writes a normalized trace line
func (w *FileDumpTraceWriter) WriteTraceLine(log *CanonicalOpLog) error {
	data := canonicalMarshal(log)
	return w.writeLine(data)
}

// WriteFiltered writes a filtered entry
func (w *FileDumpTraceWriter) WriteFiltered(reason FilterReason, log *CanonicalOpLog) error {
	if !w.includeFiltered {
		return nil
	}
	filtered := TraceDumpFiltered{
		Reason:        reason,
		Depth:         log.Depth,
		Pc:            log.Pc,
		Op:            "0x" + strconv.FormatUint(uint64(log.Op), 16),
		OpName:        log.OpName,
		FunctionDepth: log.FunctionDepth,
	}
	wrapper := map[string]*TraceDumpFiltered{"_filtered": &filtered}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// WriteStateRoot writes the stateRoot line
func (w *FileDumpTraceWriter) WriteStateRoot(stateRoot string) error {
	data := []byte(`{"stateRoot":"` + stateRoot + `"}`)
	return w.writeLine(data)
}

// WriteResult writes the final result summary
func (w *FileDumpTraceWriter) WriteResult(result *TraceDumpResult) error {
	wrapper := map[string]*TraceDumpResult{"_result": result}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// Close closes the file
func (w *FileDumpTraceWriter) Close() error {
	return w.file.Close()
}

// NullDumpTraceWriter is a no-op writer for when dumping is disabled
type NullDumpTraceWriter struct{}

func (w *NullDumpTraceWriter) WriteMeta(meta *TraceDumpMeta) error                          { return nil }
func (w *NullDumpTraceWriter) WriteTraceLine(log *CanonicalOpLog) error                     { return nil }
func (w *NullDumpTraceWriter) WriteFiltered(reason FilterReason, log *CanonicalOpLog) error { return nil }
func (w *NullDumpTraceWriter) WriteStateRoot(stateRoot string) error                        { return nil }
func (w *NullDumpTraceWriter) WriteResult(result *TraceDumpResult) error                    { return nil }
func (w *NullDumpTraceWriter) Close() error                                                 { return nil }

// StreamDumpTraceWriter writes trace dump to any io.Writer
type StreamDumpTraceWriter struct {
	writer          io.Writer
	includeFiltered bool
}

// NewStreamDumpTraceWriter creates a new stream-based trace dump writer
func NewStreamDumpTraceWriter(w io.Writer, includeFiltered bool) *StreamDumpTraceWriter {
	return &StreamDumpTraceWriter{
		writer:          w,
		includeFiltered: includeFiltered,
	}
}

func (w *StreamDumpTraceWriter) writeLine(data []byte) error {
	_, err := w.writer.Write(data)
	if err != nil {
		return err
	}
	_, err = w.writer.Write([]byte{'\n'})
	return err
}

// WriteMeta writes the metadata header
func (w *StreamDumpTraceWriter) WriteMeta(meta *TraceDumpMeta) error {
	wrapper := map[string]*TraceDumpMeta{"_meta": meta}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// WriteTraceLine writes a normalized trace line
func (w *StreamDumpTraceWriter) WriteTraceLine(log *CanonicalOpLog) error {
	data := canonicalMarshal(log)
	return w.writeLine(data)
}

// WriteFiltered writes a filtered entry
func (w *StreamDumpTraceWriter) WriteFiltered(reason FilterReason, log *CanonicalOpLog) error {
	if !w.includeFiltered {
		return nil
	}
	filtered := TraceDumpFiltered{
		Reason:        reason,
		Depth:         log.Depth,
		Pc:            log.Pc,
		Op:            "0x" + strconv.FormatUint(uint64(log.Op), 16),
		OpName:        log.OpName,
		FunctionDepth: log.FunctionDepth,
	}
	wrapper := map[string]*TraceDumpFiltered{"_filtered": &filtered}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// WriteStateRoot writes the stateRoot line
func (w *StreamDumpTraceWriter) WriteStateRoot(stateRoot string) error {
	data := []byte(`{"stateRoot":"` + stateRoot + `"}`)
	return w.writeLine(data)
}

// WriteResult writes the final result summary
func (w *StreamDumpTraceWriter) WriteResult(result *TraceDumpResult) error {
	wrapper := map[string]*TraceDumpResult{"_result": result}
	data, err := json.Marshal(wrapper)
	if err != nil {
		return err
	}
	return w.writeLine(data)
}

// Close is a no-op for stream writers (caller owns the stream)
func (w *StreamDumpTraceWriter) Close() error {
	return nil
}

// getGethVersionForNormalizer returns the geth version string
func getGethVersionForNormalizer() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	if info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			if len(setting.Value) > 8 {
				return setting.Value[:8]
			}
			return setting.Value
		}
	}
	return "dev"
}

// CreateTraceDumpMeta creates metadata for a trace dump
func CreateTraceDumpMeta(fork, inputHash, testName string) *TraceDumpMeta {
	return &TraceDumpMeta{
		Client:            "geth",
		Version:           getGethVersionForNormalizer(),
		Fork:              fork,
		InputHash:         inputHash,
		TestName:          testName,
		Timestamp:         time.Now().UTC().Format(time.RFC3339),
		NormalizerVersion: "1",
	}
}

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
	hasher     *lineCountingHasher
	prev       *CanonicalOpLog   // For geth error line merging
	dumpWriter DumpTraceWriter   // Optional dump writer for debugging
	lastLine   []byte            // Track last line for finalLineHash
}

// NewTraceNormalizer creates a new normalizer
func NewTraceNormalizer() *TraceNormalizer {
	return &TraceNormalizer{
		hasher:     newLineCountingHasher(),
		dumpWriter: &NullDumpTraceWriter{},
	}
}

// NewTraceNormalizerWithDump creates a normalizer with trace dumping enabled
func NewTraceNormalizerWithDump(writer DumpTraceWriter) *TraceNormalizer {
	return &TraceNormalizer{
		hasher:     newLineCountingHasher(),
		dumpWriter: writer,
	}
}

// SetDumpWriter sets the dump writer (can be called after creation)
func (n *TraceNormalizer) SetDumpWriter(writer DumpTraceWriter) {
	n.dumpWriter = writer
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
		n.dumpWriter.WriteFiltered(FilterReasonDepthZero, log)
		return nil
	}

	// Filter: STOP opcodes at end (geth continues on virtual STOP)
	if log.Op == 0x00 { // STOP
		n.dumpWriter.WriteFiltered(FilterReasonStopOpcode, log)
		return nil
	}

	// Handle geth duplicate line merging (same PC+depth+functionDepth = merge)
	// Geth sometimes outputs two lines for the same opcode when there's an error
	if n.prev != nil {
		if n.prev.Pc == log.Pc && n.prev.Depth == log.Depth && n.prev.FunctionDepth == log.FunctionDepth {
			// Skip this line, it's a duplicate
			n.dumpWriter.WriteFiltered(FilterReasonDuplicate, log)
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
	n.lastLine = data
	n.dumpWriter.WriteTraceLine(log)
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
		n.lastLine = []byte(stateRootJSON)
		n.dumpWriter.WriteStateRoot(stateRoot)
	}
	return n.hasher.Sum()
}

// FinishWithStateRootAndDump finishes normalization and writes the result summary to dump writer
func (n *TraceNormalizer) FinishWithStateRootAndDump(stateRoot string) []byte {
	hash := n.FinishWithStateRoot(stateRoot)

	// Compute finalLineHash (MD5 of just the last line)
	finalLineHash := ""
	if len(n.lastLine) > 0 {
		h := md5.Sum(n.lastLine)
		finalLineHash = hex.EncodeToString(h[:])
	}

	// Write result summary
	n.dumpWriter.WriteResult(&TraceDumpResult{
		TraceHash:     hex.EncodeToString(hash),
		TraceLines:    n.hasher.lines,
		FinalLineHash: finalLineHash,
	})

	return hash
}

// Lines returns the number of trace lines processed
func (n *TraceNormalizer) Lines() int {
	return n.hasher.lines
}

// Reset resets the normalizer for reuse
func (n *TraceNormalizer) Reset() {
	n.hasher.Reset()
	n.prev = nil
	n.lastLine = nil
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
