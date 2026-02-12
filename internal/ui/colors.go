// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

const (
	ColorGray       = "\033[0;90m"
	ColorReset      = "\033[0m"
	ColorYellow     = "\033[0;33m"
	ColorRed        = "\033[0;31m"
	ColorGreen      = "\033[0;32m"
	ColorCyan       = "\033[0;36m"
	ColorBlue       = "\033[1;34m"
	ColorMagenta    = "\033[1;35m"

	// Control sequences
	TermSaveCursor    = "\0337"
	TermRestoreCursor = "\0338"
	TermClearForward  = "\033[J"
)
