// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package encoding

import "io"

func wrapReaderPlatform(r io.Reader) io.Reader {
	return r
}
