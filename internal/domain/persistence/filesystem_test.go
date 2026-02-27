// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"testing"
)

func TestIsBinary(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data []byte
		want bool
	}{
		{"empty slice", []byte{}, false},
		{"plain text", []byte("hello world"), false},
		{"binary with null in middle", []byte{'a', 'b', 0, 'c'}, true},
		{"binary with null at start", []byte{0, 'a', 'b'}, true},
		{"binary with null at end", []byte{'a', 'b', 0}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsBinary(tt.data); got != tt.want {
				t.Errorf("IsBinary() = %v, want %v", got, tt.want)
			}
		})
	}
}
