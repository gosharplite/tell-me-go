// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"io"
)

// writeBestEffort writes to w, intentionally suppressing write errors.
// Terminal write failures in rendering paths are non-recoverable and
// must not crash the program. The only meaningful recovery would be
// logging to stderr, but stderr is the destination for most calls.
//
// ADR-007: UI rendering functions use best-effort I/O for display output.
// See docs/adr/007-ui-error-handling.md.
func writeBestEffort(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}
