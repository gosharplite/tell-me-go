// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package filepathutil provides cross-platform path normalization utilities
// for consistent comparison of paths from different sources (os.Getwd,
// filepath.Abs, packages.Load) that may differ in symlink resolution,
// slash direction, case, or volume prefixes on Windows.
package filepathutil

import (
	"path/filepath"
	"strings"
)

// Normalize resolves symbolic links in path and converts to forward slashes.
// When EvalSymlinks fails (e.g., the path doesn't exist yet), it recursively
// resolves the parent directory and reconstructs the path.
//
// Normalize ensures paths from different sources (os.Getwd, filepath.Abs,
// packages.Load) are in a consistent format for cross-platform comparison:
//   - Symlinks are resolved to their targets
//   - Separators are normalized to forward slashes
//   - On Windows, short names (8.3) are resolved to long names
func Normalize(path string) string {
	if path == "" {
		return ""
	}
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.ToSlash(realPath)
	}
	dir := filepath.Dir(path)
	if dir == path || dir == "." {
		return filepath.ToSlash(path)
	}
	resolvedDir := Normalize(dir)
	return filepath.ToSlash(filepath.Join(resolvedDir, filepath.Base(path)))
}

// NormalizePath resolves symlinks preserving the OS-native path format
// (no slash conversion). This is the low-level variant used when the
// result will be consumed by filepath.Rel or other OS-native operations.
func NormalizePath(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}
	dir := filepath.Dir(path)
	if dir == path || dir == "." {
		return path
	}
	resolvedDir := NormalizePath(dir)
	return filepath.Join(resolvedDir, filepath.Base(path))
}

// NormalizeKey resolves symlinks, converts to forward slashes, lowercases,
// and strips the Windows volume prefix. The result is suitable for use as
// a map key or for strings.HasPrefix comparison when paths may come from
// mixed sources (e.g., user-provided Unix-format paths vs OS-resolved paths).
func NormalizeKey(path string) string {
	s := Normalize(path)
	if vol := filepath.VolumeName(s); vol != "" {
		s = s[len(vol):]
	}
	return strings.ToLower(s)
}
