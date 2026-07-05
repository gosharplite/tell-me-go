// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "strings"

// skipLineContinuation checks whether the backslash at raw[i] begins a line
// continuation (\<LF> or \<CR><LF>). If so, it returns the new index position
// (skipping the continuation) and true. Otherwise it returns i and false.
func skipLineContinuation(raw string, i int) (newI int, skipped bool) {
	// \<LF>
	if i+1 < len(raw) && raw[i+1] == '\n' {
		return i + 1, true
	}
	// \<CR><LF>
	if i+2 < len(raw) && raw[i+1] == '\r' && raw[i+2] == '\n' {
		return i + 2, true
	}
	return i, false
}

// writeEscapedPair writes a backslash and the following character to the builder,
// advancing past both. This prevents the escaped character from being
// misinterpreted (e.g., \\\n must not see the third backslash as escaping a newline).
func writeEscapedPair(out *strings.Builder, raw string, i int) int {
	out.WriteByte('\\')
	if i+1 < len(raw) {
		out.WriteByte(raw[i+1])
		return i + 1
	}
	return i
}

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
			if newI, skipped := skipLineContinuation(raw, i); skipped {
				i = newI
				continue
			}
			i = writeEscapedPair(&out, raw, i)
		} else {
			out.WriteByte(b)
		}
	}

	return out.String()
}

// skipEscapedChar advances the index past a backslash-escaped character.
// Caller must ensure raw[i] == '\\'.
func skipEscapedChar(raw string, i int) int {
	// Skip the escaped character
	if i+1 < len(raw) {
		return i + 1
	}
	return i
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
			i = skipEscapedChar(normalized, i)
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
