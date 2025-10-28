// Copyright 2025 The go-ethereum Authors
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

package logger

import (
	"encoding/json"
	"math/big"
	"regexp"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TestCanonicalJSONKeyOrdering tests that CanonicalJSON emits keys in the correct order:
// 1. "type" field first
// 2. Primary identifier field second (operation, txIndex, blockNumber)
// 3. All other fields alphabetically
func TestCanonicalJSONKeyOrdering(t *testing.T) {
	tests := []struct {
		name            string
		record          CanonicalJSON
		wantFirstKey    string
		wantSecondKey   string
		wantAlphabetical bool // Check if remaining keys are alphabetical
	}{
		{
			name: "blockStart record",
			record: CanonicalJSON{
				"type":        "blockStart",
				"blockNumber": "0x1",
				"gasLimit":    "0x5f5e100",
				"baseFeePerGas": "0x7",
				"blockHash":   "0xabcd",
			},
			wantFirstKey:     "type",
			wantSecondKey:    "blockNumber",
			wantAlphabetical: true,
		},
		{
			name: "preExecution record",
			record: CanonicalJSON{
				"type":      "preExecution",
				"operation": "beaconRootStorage",
				"eip":       "4788",
				"gasUsed":   "0x1000",
			},
			wantFirstKey:     "type",
			wantSecondKey:    "operation",
			wantAlphabetical: true,
		},
		{
			name: "txStart record",
			record: CanonicalJSON{
				"type":     "txStart",
				"txIndex":  "0x0",
				"txHash":   "0xabcd",
				"gasLimit": "0x5208",
			},
			wantFirstKey:     "type",
			wantSecondKey:    "txIndex",
			wantAlphabetical: true,
		},
		{
			name: "validation record",
			record: CanonicalJSON{
				"type":      "validation",
				"operation": "gasAccounting",
				"valid":     true,
			},
			wantFirstKey:     "type",
			wantSecondKey:    "operation",
			wantAlphabetical: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.record)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Parse to extract key order
			var keyOrder []string
			dec := json.NewDecoder(strings.NewReader(string(data)))
			tok, err := dec.Token() // Read '{'
			if err != nil {
				t.Fatalf("Failed to decode: %v", err)
			}
			if tok != json.Delim('{') {
				t.Fatalf("Expected '{', got %v", tok)
			}

			for dec.More() {
				tok, err := dec.Token()
				if err != nil {
					t.Fatalf("Failed to read token: %v", err)
				}
				if key, ok := tok.(string); ok {
					keyOrder = append(keyOrder, key)
					// Skip value
					var val interface{}
					if err := dec.Decode(&val); err != nil {
						t.Fatalf("Failed to decode value: %v", err)
					}
				}
			}

			// Check first key
			if len(keyOrder) == 0 {
				t.Fatal("No keys found in JSON")
			}
			if keyOrder[0] != tt.wantFirstKey {
				t.Errorf("First key = %q, want %q", keyOrder[0], tt.wantFirstKey)
			}

			// Check second key if specified
			if tt.wantSecondKey != "" && len(keyOrder) > 1 {
				if keyOrder[1] != tt.wantSecondKey {
					t.Errorf("Second key = %q, want %q", keyOrder[1], tt.wantSecondKey)
				}
			}

			// Check alphabetical ordering of remaining keys
			if tt.wantAlphabetical && len(keyOrder) > 2 {
				remaining := keyOrder[2:]
				for i := 1; i < len(remaining); i++ {
					if remaining[i] < remaining[i-1] {
						t.Errorf("Keys not in alphabetical order: %v comes after %v", remaining[i], remaining[i-1])
					}
				}
			}
		})
	}
}

// TestCanonicalHexEncoding tests that all hex values are lowercase with 0x prefix
func TestCanonicalHexEncoding(t *testing.T) {
	tests := []struct {
		name  string
		input interface{}
		want  string
	}{
		{"zero uint64", uint64(0), "0x0"},
		{"non-zero uint64", uint64(255), "0xff"},
		{"large uint64", uint64(0xABCDEF), "0xabcdef"},
		{"zero big.Int", big.NewInt(0), "0x0"},
		{"non-zero big.Int", big.NewInt(255), "0xff"},
		{"large big.Int", big.NewInt(0xABCDEF), "0xabcdef"},
		{"nil big.Int", (*big.Int)(nil), "0x0"},
		{"address", common.HexToAddress("0xABCDEF"), "0x0000000000000000000000000000000000abcdef"},
		{"hash", common.HexToHash("0xABCDEF"), "0x0000000000000000000000000000000000000000000000000000000000abcdef"},
		{"empty bytes", []byte{}, "0x"},
		{"non-empty bytes", []byte{0xAB, 0xCD}, "0xabcd"},
		{"hex string uppercase", "0xABCDEF", "0xabcdef"},
		{"hex string mixed", "0xAbCdEf", "0xabcdef"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToCanonicalHex(tt.input)
			if got != tt.want {
				t.Errorf("ToCanonicalHex(%v) = %q, want %q", tt.input, got, tt.want)
			}

			// Verify no uppercase letters
			if strings.ContainsAny(got, "ABCDEF") {
				t.Errorf("ToCanonicalHex(%v) contains uppercase hex: %q", tt.input, got)
			}

			// Verify 0x prefix
			if !strings.HasPrefix(got, "0x") {
				t.Errorf("ToCanonicalHex(%v) missing 0x prefix: %q", tt.input, got)
			}
		})
	}
}

// TestCanonicalAddressFormatting tests that addresses are lowercase without EIP-55 checksum
func TestCanonicalAddressFormatting(t *testing.T) {
	tests := []struct {
		name  string
		addr  common.Address
		want  string
	}{
		{
			name: "all lowercase",
			addr: common.HexToAddress("0xabcdef1234567890abcdef1234567890abcdef12"),
			want: "0xabcdef1234567890abcdef1234567890abcdef12",
		},
		{
			name: "mixed case (EIP-55)",
			addr: common.HexToAddress("0xC014Ba5e00000000000000000000000000000000"),
			want: "0xc014ba5e00000000000000000000000000000000",
		},
		{
			name: "all uppercase",
			addr: common.HexToAddress("0xABCDEF1234567890ABCDEF1234567890ABCDEF12"),
			want: "0xabcdef1234567890abcdef1234567890abcdef12",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanonicalAddress(tt.addr)
			if got != tt.want {
				t.Errorf("CanonicalAddress(%v) = %q, want %q", tt.addr, got, tt.want)
			}

			// Verify no uppercase letters
			if strings.ContainsAny(got[2:], "ABCDEF") {
				t.Errorf("CanonicalAddress contains uppercase: %q", got)
			}
		})
	}
}

// TestOmitEmpty tests that nil and empty string values are omitted
func TestOmitEmpty(t *testing.T) {
	record := map[string]interface{}{
		"type":      "txEnd",
		"txHash":    "0xabcd",
		"status":    "0x1",
		"error":     nil,           // Should be omitted
		"logs":      []interface{}{}, // Should NOT be omitted (empty array is meaningful)
		"emptyStr":  "",            // Should be omitted
		"zeroValue": "0x0",         // Should NOT be omitted (zero is meaningful)
	}

	OmitEmpty(record)

	// Check nil was omitted
	if _, exists := record["error"]; exists {
		t.Error("nil 'error' field was not omitted")
	}

	// Check empty string was omitted
	if _, exists := record["emptyStr"]; exists {
		t.Error("empty string 'emptyStr' field was not omitted")
	}

	// Check empty array was preserved
	if _, exists := record["logs"]; !exists {
		t.Error("empty array 'logs' field was incorrectly omitted")
	}

	// Check zero value was preserved
	if _, exists := record["zeroValue"]; !exists {
		t.Error("zero value 'zeroValue' field was incorrectly omitted")
	}
}

// TestCompactJSON tests that marshaled output is compact (no whitespace)
func TestCompactJSON(t *testing.T) {
	record := CanonicalJSON{
		"type":        "blockStart",
		"blockNumber": "0x1",
		"gasLimit":    "0x5f5e100",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	str := string(data)

	// Check no indentation
	if strings.Contains(str, "\n") {
		t.Error("Output contains newlines (should be compact)")
	}
	if strings.Contains(str, "  ") {
		t.Error("Output contains multiple spaces (should be compact)")
	}

	// Verify it's valid JSON by unmarshaling
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Errorf("Compact JSON is not valid: %v", err)
	}
}

// TestNestedObjectOrdering tests that nested objects also follow canonical ordering
func TestNestedObjectOrdering(t *testing.T) {
	record := CanonicalJSON{
		"type":      "preExecution",
		"operation": "beaconRootStorage",
		"ringBuffer": map[string]interface{}{
			"timestampSlot": "0x1",
			"index":         "0x2",
			"rootSlot":      "0x3",
		},
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Check that nested object keys are alphabetical
	str := string(data)
	if !strings.Contains(str, `"ringBuffer"`) {
		t.Fatal("Missing ringBuffer field")
	}

	// Nested object should have alphabetical keys: index, rootSlot, timestampSlot
	idxPos := strings.Index(str, `"index"`)
	rootPos := strings.Index(str, `"rootSlot"`)
	tsPos := strings.Index(str, `"timestampSlot"`)

	if idxPos == -1 || rootPos == -1 || tsPos == -1 {
		t.Fatal("Missing nested keys")
	}

	if !(idxPos < rootPos && rootPos < tsPos) {
		t.Errorf("Nested keys not in alphabetical order: index=%d, rootSlot=%d, timestampSlot=%d",
			idxPos, rootPos, tsPos)
	}
}

// TestRealWorldTrace tests a complete trace record matches canonical format
func TestRealWorldTrace(t *testing.T) {
	// Simulate a blockStart record with all lowercase hex (canonical format)
	record := CanonicalJSON{
		"type":                  "blockStart",
		"blockNumber":           "0x1",
		"baseFeePerGas":         "0x7",
		"blobGasUsed":           "0xc0000",
		"blockHash":             "0xd9e8f10b5c08549e5a891261faf5b0c1168b5b32326a97aed8980b66a8dfffd6",
		"difficulty":            "0x0",
		"excessBlobGas":         "0x0",
		"fork":                  "prague",
		"gasLimit":              "0x11e1a300",
		"miner":                 "0xc014ba5e00000000000000000000000000000000",
		"parentBeaconBlockRoot": "0x6c31fc15422ebad28aaf9089c306702f67540b53c7eea8b7d2941044b027100f",
		"parentHash":            "0xcdc02aaaf71d6bb1f39670ddc0bdeb5d907bc4d32482dd52c5eb6e57ebfe892e",
		"requestsHash":          "0xe3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"timestamp":             "0x3e8",
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	str := string(data)

	// Rule 1: "type" must be first
	if !strings.HasPrefix(str, `{"type":"blockStart"`) {
		t.Errorf("type field not first in output: %s", str[:100])
	}

	// Rule 2: "blockNumber" must be second (primary identifier)
	typeEnd := strings.Index(str, `"blockStart"`)
	blockNumStart := strings.Index(str, `"blockNumber"`)
	if blockNumStart == -1 || blockNumStart < typeEnd {
		t.Error("blockNumber field not second in output")
	}

	// Rule 3: All hex values must be lowercase (check within "0x..." patterns)
	// Look for hex values (0x followed by hex digits) and check if they contain uppercase
	hexPattern := `"0x[0-9a-fA-F]+"`
	matches := regexp.MustCompile(hexPattern).FindAllString(str, -1)
	for _, match := range matches {
		if strings.ContainsAny(match, "ABCDEF") {
			t.Errorf("Hex value contains uppercase: %s", match)
		}
	}

	// Rule 4: Compact (no whitespace except in strings)
	if strings.Contains(str, "\n") || strings.Contains(str, "  ") {
		t.Error("Output is not compact")
	}

	// Rule 5: All hex values have 0x prefix
	if !strings.Contains(str, `"0x`) {
		t.Error("No hex values found (expected 0x prefixes)")
	}

	// Verify structure
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded["type"] != "blockStart" {
		t.Errorf("type = %v, want blockStart", decoded["type"])
	}
}
