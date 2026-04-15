// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package encoding

import (
	"io"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestIsUTF8Env(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		expected bool
	}{
		{
			name: "LC_ALL=en_US.UTF-8",
			env: map[string]string{
				"LC_ALL": "en_US.UTF-8",
			},
			expected: true,
		},
		{
			name: "LC_CTYPE=UTF-8",
			env: map[string]string{
				"LC_CTYPE": "UTF-8",
			},
			expected: true,
		},
		{
			name: "LANG=C.UTF-8",
			env: map[string]string{
				"LANG": "C.UTF-8",
			},
			expected: true,
		},
		{
			name: "LANG=zh_TW.Big5",
			env: map[string]string{
				"LANG": "zh_TW.Big5",
			},
			expected: false,
		},
		{
			name:     "No environment variables set",
			env:      map[string]string{},
			expected: false,
		},
		{
			name: "Variables set to empty string",
			env: map[string]string{
				"LANG": "",
			},
			expected: false,
		},
		{
			name: "Case sensitivity check: lang=utf-8",
			env: map[string]string{
				"LANG": "utf-8",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(key string) string {
				return tt.env[key]
			}
			got := isUTF8Env(getenv)
			if got != tt.expected {
				t.Errorf("isUTF8Env() = %v; want %v", got, tt.expected)
			}
		})
	}
}

func TestDecodeBytes(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			name:     "valid UTF-8",
			input:    []byte("hello world"),
			expected: []byte("hello world"),
		},
		{
			name:     "empty slice",
			input:    []byte{},
			expected: []byte{},
		},
		{
			name:     "invalid UTF-8 sequence",
			input:    []byte{0xff, 0xfe, 0xfd},
			expected: []byte{0xff, 0xfe, 0xfd}, // On non-Windows, it returns as-is
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecodeBytes(tt.input)

			// On Windows, DecodeBytes attempts to decode based on the system locale,
			// so an invalid UTF-8 sequence will be converted to valid UTF-8 if possible,
			// or replaced with replacement characters.
			if runtime.GOOS == "windows" && tt.name == "invalid UTF-8 sequence" {
				// On Windows, the output is valid UTF-8 if decoded via a legacy code page,
				// or identical to the input if the system already uses UTF-8 (CP 65001).
				if !utf8.Valid(got) && string(got) != string(tt.expected) {
					t.Errorf("got %v; want valid UTF-8 or raw input (%v)", got, tt.expected)
				}
				return
			}

			if string(got) != string(tt.expected) {
				t.Errorf("got %v; want %v", got, tt.expected)
			}
		})
	}
}
