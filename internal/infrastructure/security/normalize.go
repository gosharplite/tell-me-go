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

// quoteTracker tracks whether the current scan position is inside single
// or double quotes. It handles backslash escape skipping (outside single
// quotes) and quote-state toggling for ' and " characters.
type quoteTracker struct {
	inSingle bool
	inDouble bool
}

// handleBackslash processes a backslash character. Outside single quotes,
// the backslash escapes the next character. Returns 1 to skip the next
// byte, or 0 if the backslash should be treated literally.
func (qt *quoteTracker) handleBackslash() (skip int) {
	if !qt.inSingle {
		return 1
	}
	return 0
}

// toggleQuote toggles quote state for ' and " characters.
func (qt *quoteTracker) toggleQuote(b byte) {
	switch b {
	case '\'':
		if !qt.inDouble {
			qt.inSingle = !qt.inSingle
		}
	case '"':
		if !qt.inSingle {
			qt.inDouble = !qt.inDouble
		}
	}
}

// isQuoted returns true when the scanner is inside single or double quotes.
func (qt *quoteTracker) isQuoted() bool {
	return qt.inSingle || qt.inDouble
}

// hasBareNewline performs a quote-aware scan of an already-normalized string
// and reports whether it contains an unquoted (bare) newline character.
// Such newlines can be used for command injection and must be rejected.
func hasBareNewline(normalized string) bool {
	var qt quoteTracker

	for i := 0; i < len(normalized); i++ {
		switch normalized[i] {
		case '\\':
			i += qt.handleBackslash()
		case '\'', '"':
			qt.toggleQuote(normalized[i])
		case '\n':
			if !qt.isQuoted() {
				return true
			}
		}
	}

	return false
}
