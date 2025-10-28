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
	"bytes"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

// CanonicalJSON represents a JSON object that will be marshaled with canonical key ordering.
// According to the EIP Block-Level Execution Trace Specification:
// 1. The "type" field MUST appear first
// 2. The primary identifier field (e.g., "operation", "txIndex", "blockNumber") MUST appear second
// 3. All remaining fields MUST appear in alphabetical order
//
// This ensures byte-identical trace output across different Ethereum clients,
// enabling direct hash-based comparison for differential testing.
type CanonicalJSON map[string]interface{}

// MarshalJSON implements the json.Marshaler interface with canonical key ordering.
// It produces compact JSON (no whitespace) with keys ordered according to the specification.
func (c CanonicalJSON) MarshalJSON() ([]byte, error) {
	if len(c) == 0 {
		return []byte("{}"), nil
	}

	// Extract all keys
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}

	// Sort with custom ordering: "type" first, primary identifier second, rest alphabetical
	primaryField := getPrimaryField(c)
	sort.Slice(keys, func(i, j int) bool {
		// "type" always comes first
		if keys[i] == "type" {
			return true
		}
		if keys[j] == "type" {
			return false
		}
		// Primary identifier field comes second (after "type")
		if primaryField != "" {
			if keys[i] == primaryField {
				return true
			}
			if keys[j] == primaryField {
				return false
			}
		}
		// All other fields in alphabetical order
		return keys[i] < keys[j]
	})

	// Build compact JSON manually to ensure canonical formatting
	var buf bytes.Buffer
	buf.WriteByte('{')

	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}

		// Marshal key
		keyJSON, err := json.Marshal(k)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal key %q: %w", k, err)
		}
		buf.Write(keyJSON)
		buf.WriteByte(':')

		// Marshal value with canonical formatting
		valJSON, err := marshalCanonicalValue(c[k])
		if err != nil {
			return nil, fmt.Errorf("failed to marshal value for key %q: %w", k, err)
		}
		buf.Write(valJSON)
	}

	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// getPrimaryField returns the primary identifier field for a record type.
// This is the field that should appear second (after "type") in canonical ordering.
func getPrimaryField(obj map[string]interface{}) string {
	recordType, ok := obj["type"].(string)
	if !ok {
		return ""
	}

	switch recordType {
	case "preExecution", "postExecution", "validation", "trieOperation":
		return "operation"
	case "txStart", "txEnd":
		return "txIndex"
	case "blockStart", "blockEnd":
		return "blockNumber"
	default:
		return ""
	}
}

// marshalCanonicalValue marshals a value with canonical formatting rules:
// - Nested objects: recursively apply canonical key ordering
// - Arrays: maintain element order, recursively canonicalize elements
// - Strings: ensure lowercase hex for addresses and hashes
// - Numbers: already handled by Go's json.Marshal
// - Booleans, null: handled by Go's json.Marshal
func marshalCanonicalValue(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case map[string]interface{}:
		// Recursively canonicalize nested objects
		canonical := CanonicalJSON(val)
		return json.Marshal(canonical)

	case CanonicalJSON:
		// Already canonical
		return json.Marshal(val)

	case []interface{}:
		// Arrays: maintain order but canonicalize elements
		result := make([]json.RawMessage, len(val))
		for i, elem := range val {
			elemJSON, err := marshalCanonicalValue(elem)
			if err != nil {
				return nil, err
			}
			result[i] = elemJSON
		}
		return json.Marshal(result)

	case []map[string]interface{}:
		// Array of objects: canonicalize each object
		result := make([]CanonicalJSON, len(val))
		for i, elem := range val {
			result[i] = CanonicalJSON(elem)
		}
		return json.Marshal(result)

	default:
		// Primitive types: use standard JSON marshaling
		return json.Marshal(v)
	}
}

// ToCanonicalHex converts a value to canonical lowercase hex string with 0x prefix.
// Handles: uint64, *big.Int, common.Address, common.Hash, []byte
func ToCanonicalHex(v interface{}) string {
	switch val := v.(type) {
	case uint64:
		if val == 0 {
			return "0x0"
		}
		return fmt.Sprintf("0x%x", val)

	case *big.Int:
		if val == nil || val.Sign() == 0 {
			return "0x0"
		}
		return "0x" + val.Text(16)

	case common.Address:
		return strings.ToLower(val.Hex())

	case common.Hash:
		return strings.ToLower(val.Hex())

	case []byte:
		if len(val) == 0 {
			return "0x"
		}
		return "0x" + common.Bytes2Hex(val)

	case string:
		// If already hex, ensure lowercase
		if strings.HasPrefix(val, "0x") || strings.HasPrefix(val, "0X") {
			return "0x" + strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(val, "0x"), "0X"))
		}
		return val

	default:
		// Fallback for unknown types
		return fmt.Sprintf("%v", v)
	}
}

// OmitEmpty removes fields with nil or empty string values from a map.
// This implements the canonical format rule: "Optional fields with null values MUST be omitted"
//
// Note: Empty arrays ([]) and zero values ("0x0") are NOT omitted per spec.
// This function works with both CanonicalJSON and regular map[string]interface{}.
func OmitEmpty(m map[string]interface{}) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}

		// Omit empty strings (but not "0x" or "0x0" which are valid zero values)
		if s, ok := v.(string); ok && s == "" {
			delete(m, k)
		}
	}
}

// OmitEmptyCanonical is like OmitEmpty but preserves CanonicalJSON type.
func OmitEmptyCanonical(m CanonicalJSON) {
	for k, v := range m {
		if v == nil {
			delete(m, k)
			continue
		}

		// Omit empty strings (but not "0x" or "0x0" which are valid zero values)
		if s, ok := v.(string); ok && s == "" {
			delete(m, k)
		}
	}
}

// CanonicalAddress ensures an address is formatted in canonical lowercase form.
func CanonicalAddress(addr common.Address) string {
	return strings.ToLower(addr.Hex())
}

// CanonicalHash ensures a hash is formatted in canonical lowercase form.
func CanonicalHash(hash common.Hash) string {
	return strings.ToLower(hash.Hex())
}

// CanonicalBytes converts bytes to canonical hex string (lowercase with 0x prefix).
func CanonicalBytes(b []byte) string {
	if len(b) == 0 {
		return "0x"
	}
	return "0x" + common.Bytes2Hex(b)
}
