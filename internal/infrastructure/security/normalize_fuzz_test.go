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
	inSingle := false
	inDouble := false
	start := -1
	for i := 0; i < len(s); i++ {
		b := s[i]
		switch {
		case b == '\\' && !inSingle:
			i++ // skip escaped char
		case b == '\'' && !inDouble:
			if inSingle {
				regions = append(regions, s[start:i+1])
				inSingle = false
			} else {
				start = i
				inSingle = true
			}
		case b == '"' && !inSingle:
			inDouble = !inDouble
		}
	}
	// If input ends inside single quotes, still capture it
	if inSingle && start >= 0 {
		regions = append(regions, s[start:])
	}
	return regions
}
