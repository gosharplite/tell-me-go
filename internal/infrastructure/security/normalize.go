// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "strings"

// normalize is a quote-aware pure-filter state machine that strips
// backslash-newline line continuations from raw command strings.
//
// Inside single quotes, backslashes are literal and pass through unchanged.
// Inside double quotes and unquoted contexts, a backslash followed by
// <LF> or <CR><LF> is treated as a line continuation and removed.
func normalize(raw string) string {
	var out strings.Builder
	out.Grow(len(raw))

	inSingle := false
	inDouble := false

	for i := 0; i < len(raw); i++ {
		b := raw[i]

		if b == '\'' && !inDouble {
			inSingle = !inSingle
			out.WriteByte(b)
		} else if b == '"' && !inSingle {
			inDouble = !inDouble
			out.WriteByte(b)
		} else if b == '\\' && !inSingle {
			// First check if the backslash itself is escaped.
			if i > 0 && raw[i-1] == '\\' {
				out.WriteByte(b)
				continue
			}
			// Line continuation: \<LF>
			if i+1 < len(raw) && raw[i+1] == '\n' {
				i++ // skip \n; for-loop i++ advances past it
				continue
			}
			// Line continuation: \<CR><LF>
			if i+2 < len(raw) && raw[i+1] == '\r' && raw[i+2] == '\n' {
				i += 2 // skip \r\n; for-loop i++ advances past them
				continue
			}
			out.WriteByte(b)
		} else {
			out.WriteByte(b)
		}
	}

	return out.String()
}

// hasBareNewline performs a quote-aware scan of an already-normalized string
// and reports whether it contains an unquoted (bare) newline character.
// Such newlines can be used for command injection and must be rejected.
func hasBareNewline(normalized string) bool {
	inSingle := false
	inDouble := false

	for i := 0; i < len(normalized); i++ {
		b := normalized[i]

		if b == '\\' && !inSingle {
			i++ // skip escaped character
		} else if b == '\'' && !inDouble {
			inSingle = !inSingle
		} else if b == '"' && !inSingle {
			inDouble = !inDouble
		} else if b == '\n' && !inSingle && !inDouble {
			return true
		}
	}

	return false
}
