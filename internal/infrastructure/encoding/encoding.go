// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package encoding

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

// WrapReader wraps an io.Reader to decode it to UTF-8 based on the current system's locale.
// On non-Windows platforms, it currently returns the reader as-is.
func WrapReader(r io.Reader) io.Reader {
	return wrapReaderPlatform(r)
}

// DecodeBytes decodes a byte slice to UTF-8 based on the current system's locale.
// If the input is already valid UTF-8, it returns the input as-is.
func DecodeBytes(b []byte) []byte {
	if len(b) == 0 || utf8.Valid(b) {
		return b
	}
	return decodeFromReader(WrapReader(bytes.NewReader(b)), b)
}

// decodeFromReader reads all bytes from r, falling back to fallback on error.
func decodeFromReader(r io.Reader, fallback []byte) []byte {
	decoded, err := io.ReadAll(r)
	if err != nil {
		return fallback
	}
	return decoded
}

func isUTF8Env(getenv func(string) string) bool {
	for _, env := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if val := getenv(env); val != "" {
			if strings.Contains(strings.ToUpper(val), "UTF-8") {
				return true
			}
		}
	}
	return false
}
