// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzValidatePath(f *testing.F) {
	policy := NewPathPolicy()
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

		if path == "" {
			if validated != "" {
				t.Errorf("ValidatePath(\"\") returned %q, want \"\"", validated)
			}
			return
		}

		// Success criteria:
		// 1. The path must be absolute
		if !filepath.IsAbs(validated) {
			t.Errorf("ValidatePath returned non-absolute path: %q", validated)
		}

		// 2. The path must be cleaned (no .. or . components)
		if validated != filepath.Clean(validated) {
			t.Errorf("ValidatePath returned uncleaned path: %q", validated)
		}

		// 3. The path must be within one of the authorized boundaries
		absValidated := validated

		inCWD, _ := policy.checkBoundary(absValidated, cwd)
		inTemp, _ := policy.checkBoundary(absValidated, temp)
		
		inSafe := false
		for _, sp := range policy.GetPaths(true) {
			if ok, _ := policy.checkBoundary(absValidated, sp); ok {
				inSafe = true
				break
			}
		}

		inReadOnly := false
		if !writable {
			for _, rop := range policy.GetPaths(false) {
				if ok, _ := policy.checkBoundary(absValidated, rop); ok {
					inReadOnly = true
					break
				}
			}
		}

		if !inCWD && !inTemp && !inSafe && !inReadOnly {
			t.Errorf("SECURITY VIOLATION: Path %q (validated as %q, writable=%v) is outside all allowed boundaries.\nCWD: %s\nTemp: %s\nSafe: %v\nReadOnly: %v",
				path, validated, writable, cwd, temp, policy.GetPaths(true), policy.GetPaths(false))
		}
	})
}
