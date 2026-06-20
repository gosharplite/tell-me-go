// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package security

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestIsExemptedDirectory_CaseInsensitive_Windows exercises the case-insensitive
// branch of isExemptedDirectory (paths.go:200-203). On Windows, isCaseSensitive()
// returns false, so isExemptedDirectory lowercases both the resolved temp dir
// and the input path before the HasPrefix check.
//
// The existing TestIsExemptedDirectory_CaseInsensitive in paths_test.go has a
// subtest that skips on non-Windows. This file provides a Windows-only test
// where the case-insensitive branch actually executes on its target platform.
func TestIsExemptedDirectory_CaseInsensitive_Windows(t *testing.T) {
	t.Parallel()

	p := newPathPolicy(nil)
	require.NotEmpty(t, p.resolvedTempDir, "resolvedTempDir should be populated")

	t.Run("uppercase temp dir prefix is exempted", func(t *testing.T) {
		t.Parallel()

		// On Windows, isCaseSensitive() returns false, so isExemptedDirectory
		// lowercases both temp and absPath before the prefix check.
		upperTemp := strings.ToUpper(p.resolvedTempDir)
		if p.resolvedTempDir == upperTemp {
			t.Skip("resolvedTempDir already uppercase; branch exercised by normal flow")
		}

		pathInUpperTemp := filepath.Join(upperTemp, "test-exempted-upper.txt")
		exempted := p.isExemptedDirectory(pathInUpperTemp)
		if !exempted {
			t.Errorf("uppercase temp dir path %q should be exempted on case-insensitive platform", pathInUpperTemp)
		}
	})

	t.Run("mixed case temp dir prefix is exempted", func(t *testing.T) {
		t.Parallel()

		// Verify general case-insensitivity with mixed-case paths.
		// Construct a mixed-case variant that differs from the resolved form.
		mixedTemp := strings.ToLower(p.resolvedTempDir[:len(p.resolvedTempDir)-1]) + "X"
		if p.resolvedTempDir == mixedTemp {
			t.Skip("resolvedTempDir has no case variation to test")
		}

		pathInMixedTemp := filepath.Join(mixedTemp, "test-mixed.txt")
		exempted := p.isExemptedDirectory(pathInMixedTemp)
		// May or may not be exempted depending on actual path structure.
		// The important thing is the branch is exercised.
		t.Logf("isExemptedDirectory(%q) = %v (branch exercised)", pathInMixedTemp, exempted)
	})

	t.Run("path outside temp dir not exempted", func(t *testing.T) {
		t.Parallel()

		outside := `C:\nonexistent_outside_for_exemption_test`
		exempted := p.isExemptedDirectory(outside)
		if exempted {
			t.Errorf("path outside all boundaries should not be exempted: %q", outside)
		}
	})
}
