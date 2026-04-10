// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package encoding

import (
	"io"
	"strings"
)

// WrapReader wraps an io.Reader to decode it to UTF-8 based on the current system's locale.
// On non-Windows platforms, it currently returns the reader as-is.
func WrapReader(r io.Reader) io.Reader {
	return wrapReaderPlatform(r)
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
