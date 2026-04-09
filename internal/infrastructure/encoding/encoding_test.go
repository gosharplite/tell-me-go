// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package encoding

import (
	"io"
	"strings"
	"testing"
)

func TestWrapReader(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "simple string",
			input: "hello world",
		},
		{
			name:  "UTF-8 string",
			input: "こんにちは世界", // Hello World in Japanese
		},
		{
			name:  "empty string",
			input: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := strings.NewReader(tt.input)
			wrapped := WrapReader(r)

			if wrapped == nil {
				t.Fatal("WrapReader returned nil")
			}

			gotBytes, err := io.ReadAll(wrapped)
			if err != nil {
				t.Fatalf("failed to read from wrapped reader: %v", err)
			}

			got := string(gotBytes)
			if got != tt.input {
				t.Errorf("got %q; want %q", got, tt.input)
			}
		})
	}
}
