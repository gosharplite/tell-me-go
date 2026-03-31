// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"testing"
)

func TestSafeBuffer_Contract(t *testing.T) {
	sb := &SafeBuffer{}

	// Test Write and Len
	data := []byte("architect")
	n, err := sb.Write(data)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(data) {
		t.Errorf("Write returned n=%d; want %d", n, len(data))
	}
	if sb.Len() != len(data) {
		t.Errorf("Len() = %d; want %d", sb.Len(), len(data))
	}

	// Test Bytes and String
	if string(sb.Bytes()) != string(data) {
		t.Errorf("Bytes() content mismatch: got %q; want %q", sb.Bytes(), data)
	}
	if sb.String() != string(data) {
		t.Errorf("String() mismatch: got %q; want %q", sb.String(), data)
	}

	// Test Reset
	sb.Reset()
	if sb.Len() != 0 {
		t.Errorf("Reset failed: Len() = %d; want 0", sb.Len())
	}
	if sb.String() != "" {
		t.Errorf("Reset failed: String() = %q; want empty", sb.String())
	}
}
