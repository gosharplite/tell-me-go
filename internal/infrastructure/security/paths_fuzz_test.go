// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzValidatePath(f *testing.F) {
	policy := newPathPolicy(nil)
	cwd, _ := os.Getwd()
	temp := os.TempDir()

	// Register some specific safe/readonly paths
	safeDir := filepath.Join(cwd, "fuzz_safe")
	readOnlyDir := filepath.Join(cwd, "fuzz_readonly")
	policy.RegisterPath(safeDir, true)
	policy.RegisterPath(readOnlyDir, false)

	// Seed with various patterns
	seeds := []string{
		"/",
		"/etc/shadow",
		"../../etc/shadow",
		cwd,
		temp,
		filepath.Join(cwd, "test.txt"),
		"test.txt",
		"./test.txt",
		"\x00",
		"C:\\Windows\\System32",
		"relative/path",
		"../relative/path",
		"/tmp/test",
		// Seeds targeting gap-documented code paths (issue #830):
		// - NUL byte variants for filepath.Abs error branches
		"\x00/tmp/test",
		"/tmp/\x00test",
		// - Case-variant paths (exercises Windows case-insensitive branches)
		"/TMP/TEST",
		"/PRIVATE/TMP/TEST",
		// - Extra temp dir paths (exercises macOS /private/tmp branch)
		"/private/tmp/test",
		"/private/tmp/nested/file",
		// - Paths that look like temp dirs but aren't (boundary-exercise)
		"/var/tmp/test",
		safeDir,
		filepath.Join(safeDir, "inner"),
		readOnlyDir,
		filepath.Join(readOnlyDir, "inner"),
		" ",
		".",
		"..",
	}

	for _, s := range seeds {
		f.Add(s, true)
		f.Add(s, false)
	}

	f.Fuzz(func(t *testing.T, path string, writable bool) {
		validated, err := policy.ValidatePath(path, writable)
		if err != nil {
			// Most random strings should fail validation
			return
		}

		verifyPathBoundaries(t, policy, path, validated, writable, cwd, temp)
	})
}

func verifyPathBoundaries(t *testing.T, policy *pathPolicy, originalPath, validatedPath string, writable bool, cwd, temp string) {
	t.Helper()

	if originalPath == "" {
		if validatedPath != "" {
			t.Errorf("ValidatePath(\"\") returned %q, want \"\"", validatedPath)
		}
		return
	}

	// 1. The path must be absolute
	if !filepath.IsAbs(validatedPath) {
		t.Errorf("ValidatePath returned non-absolute path: %q", validatedPath)
	}

	// 2. The path must be cleaned (no .. or . components)
	if validatedPath != filepath.Clean(validatedPath) {
		t.Errorf("ValidatePath returned uncleaned path: %q", validatedPath)
	}

	// 3. The path must be within one of the authorized boundaries
	allowed := []string{cwd, temp}
	allowed = append(allowed, getExtraTempDirs()...)
	allowed = append(allowed, policy.GetPaths(true)...)
	if !writable {
		allowed = append(allowed, policy.GetPaths(false)...)
	}

	inBoundary := false
	for _, b := range allowed {
		if ok, _ := policy.checkBoundary(validatedPath, b); ok {
			inBoundary = true
			break
		}
	}

	if !inBoundary {
		t.Errorf("SECURITY VIOLATION: Path %q (validated as %q, writable=%v) is outside all allowed boundaries.\nCWD: %s\nTemp: %s\nSafe: %v\nReadOnly: %v",
			originalPath, validatedPath, writable, cwd, temp, policy.GetPaths(true), policy.GetPaths(false))
	}
}

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
