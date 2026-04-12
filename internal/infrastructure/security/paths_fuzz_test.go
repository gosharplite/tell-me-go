// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
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
