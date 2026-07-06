// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"strings"
	"testing"
)

// FuzzNormalizeSingleQuoted verifies that bytes inside single-quoted
// regions are never altered by the normalize pre-processor (Phase 2
// idempotency & quote-preservation invariant).
func FuzzNormalizeSingleQuoted(f *testing.F) {
	// Seed corpus: various single-quoted strings with embedded continuations
	f.Add("echo 'hello world'")
	f.Add("echo 'lit\\\neral'")
	f.Add("echo 'backslash\\\\preserved'")
	f.Add("echo '\\\n'")
	f.Add("'single quoted \\\n \\\r\n \\\\'")
	f.Add("mixed 'quoted' and unquoted \\\n continuation")

	f.Fuzz(func(t *testing.T, input string) {
		// Extract all single-quoted regions from the input
		singleQuotedRegions := extractSingleQuotedRegions(input)

		normalized := normalize(input)

		// Every single-quoted region from the original must appear
		// byte-for-byte unchanged in the normalized output.
		for _, region := range singleQuotedRegions {
			if !strings.Contains(normalized, region) {
				t.Errorf("single-quoted region %q from input %q not preserved in normalized output %q",
					region, input, normalized)
			}
		}

		// Idempotency: Normalize(Normalize(s)) == Normalize(s)
		doubleNorm := normalize(normalized)
		if normalized != doubleNorm {
			t.Errorf("idempotency violated for input %q:\n  first:  %q\n  second: %q",
				input, normalized, doubleNorm)
		}
	})
}

// extractSingleQuotedRegions returns all byte sequences between unescaped
// single quotes in the input. It handles backslash escaping outside single
// quotes (so \" or \' doesn't break detection).
func extractSingleQuotedRegions(s string) []string {
	var regions []string
	var qt quoteTracker
	start := -1

	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i += qt.handleBackslash()
		case '\'':
			captureSingleQuoteRegion(&qt, s, i, &start, &regions)
		case '"':
			qt.toggleQuote(s[i])
		}
	}

	if qt.inSingle {
		regions = append(regions, s[start:])
	}
	return regions
}

// captureSingleQuoteRegion processes a single-quote character during
// region extraction. When outside double quotes, it either opens a new
// region (recording start) or closes the current region (appending the
// captured slice). The quoteTracker's inSingle flag is toggled.
func captureSingleQuoteRegion(qt *quoteTracker, s string, i int, start *int, regions *[]string) {
	if !qt.inDouble {
		if qt.inSingle {
			*regions = append(*regions, s[*start:i+1])
		} else {
			*start = i
		}
		qt.inSingle = !qt.inSingle
	}
}
