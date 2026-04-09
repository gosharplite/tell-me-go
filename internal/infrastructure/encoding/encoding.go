// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package encoding

import (
	"io"
)

// WrapReader wraps an io.Reader to decode it to UTF-8 based on the current system's locale.
// On non-Windows platforms, it currently returns the reader as-is.
func WrapReader(r io.Reader) io.Reader {
	return wrapReaderPlatform(r)
}
