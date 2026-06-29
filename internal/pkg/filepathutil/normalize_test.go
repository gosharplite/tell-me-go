// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package filepathutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize_ExistingPath(t *testing.T) {
	tmpDir := t.TempDir()

	realFile := filepath.Join(tmpDir, "real.txt")
	if err := os.WriteFile(realFile, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	result := Normalize(realFile)

	// Normalize must return a forward-slashed path with symlinks resolved.
	// EvalSymlinks may change the path format (e.g., short→long names on Windows),
	// so we verify properties rather than exact string match.
	if result == "" {
		t.Error("Normalize returned empty string")
	}
	if !strings.Contains(result, "real.txt") {
		t.Errorf("Normalize(%q) = %q; missing filename", realFile, result)
	}
	if strings.Contains(result, "\\") {
		t.Errorf("Normalize(%q) = %q; contains backslashes", realFile, result)
	}

	// Normalize must be idempotent
	result2 := Normalize(result)
	if result != result2 {
		t.Errorf("Normalize not idempotent: first=%q second=%q", result, result2)
	}
}

func TestNormalize_NonExistentPath(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistent := filepath.Join(tmpDir, "nonexistent", "file.txt")

	result := Normalize(nonExistent)

	if result == "" {
		t.Error("Normalize returned empty string for non-existent path")
	}
	if !strings.HasSuffix(result, "nonexistent/file.txt") {
		t.Errorf("Normalize(%q) = %q; expected suffix 'nonexistent/file.txt'", nonExistent, result)
	}
}

func TestNormalize_BareFilename(t *testing.T) {
	result := Normalize("barefile.txt")
	if result != "barefile.txt" {
		t.Errorf("Normalize(%q) = %q; want %q", "barefile.txt", result, "barefile.txt")
	}
}

func TestNormalize_EmptyString(t *testing.T) {
	result := Normalize("")
	if result != "" {
		t.Errorf("Normalize(%q) = %q; want empty", "", result)
	}
}

func TestNormalize_RootDirectory(t *testing.T) {
	result := Normalize("/")
	if !strings.HasSuffix(result, "/") && result != "/" {
		t.Errorf("Normalize(/) = %q; expected root path", result)
	}
}

func TestNormalizePath_PreservesOSFormat(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")

	result := NormalizePath(path)
	if result == "" {
		t.Error("NormalizePath returned empty string")
	}
}

func TestNormalizeKey_StripsVolume(t *testing.T) {
	result := NormalizeKey("/some/path/file.go")
	if strings.Contains(result, ":") {
		t.Errorf("NormalizeKey(%q) = %q; expected no volume prefix", "/some/path/file.go", result)
	}
	if result != strings.ToLower("/some/path/file.go") {
		t.Errorf("NormalizeKey = %q; want lowercased path", result)
	}
}

func TestNormalizeKey_IsLowercase(t *testing.T) {
	tmpDir := t.TempDir()
	upperFile := filepath.Join(tmpDir, "UPPERCASE.go")
	if err := os.WriteFile(upperFile, []byte("package p"), 0644); err != nil {
		t.Fatal(err)
	}

	result := NormalizeKey(upperFile)
	if result != strings.ToLower(result) {
		t.Errorf("NormalizeKey(%q) = %q; expected all lowercase", upperFile, result)
	}
}

func TestNormalizeKey_ConsistentForSameDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	a := filepath.Join(tmpDir, "a.go")
	b := filepath.Join(tmpDir, "b.go")

	keyA := NormalizeKey(a)
	keyB := NormalizeKey(b)

	// Strip filename to get the directory portion for comparison
	dirA := keyA[:len(keyA)-len("/a.go")]
	dirB := keyB[:len(keyB)-len("/b.go")]

	if dirA != dirB {
		t.Errorf("directory portions differ: %q vs %q", dirA, dirB)
	}
}

func TestNormalize_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.go")
	if err := os.WriteFile(path, []byte("package p"), 0644); err != nil {
		t.Fatal(err)
	}

	first := Normalize(path)
	second := Normalize(first)
	if first != second {
		t.Errorf("Normalize not idempotent: %q vs %q", first, second)
	}
}

func TestNormalizeForCompare_WindowsRoot(t *testing.T) {
	// C:/ must normalize to / without touching the filesystem.
	// Volume prefix is stripped, leaving just the root separator.
	result := NormalizeForCompare("C:/")
	if result != "/" {
		t.Errorf("NormalizeForCompare(%q) = %q; want %q", "C:/", result, "/")
	}
}

func TestNormalizeForCompare_StripsVolume(t *testing.T) {
	result := NormalizeForCompare("D:\\Users\\foo\\bar.txt")
	if result != "/users/foo/bar.txt" {
		t.Errorf("NormalizeForCompare = %q; want %q", result, "/users/foo/bar.txt")
	}
}

func TestNormalizeForCompare_WindowsChildPath(t *testing.T) {
	result := NormalizeForCompare("C:\\a\\b.go")
	if result != "/a/b.go" {
		t.Errorf("NormalizeForCompare = %q; want %q", result, "/a/b.go")
	}
}

func TestNormalizeForCompare_UnixPath(t *testing.T) {
	result := NormalizeForCompare("/usr/local/bin")
	if result != "/usr/local/bin" {
		t.Errorf("NormalizeForCompare = %q; want %q", result, "/usr/local/bin")
	}
}

func TestNormalizeForCompare_Lowercases(t *testing.T) {
	result := NormalizeForCompare("/Foo/Bar/BAZ.go")
	if result != "/foo/bar/baz.go" {
		t.Errorf("NormalizeForCompare = %q; want %q", result, "/foo/bar/baz.go")
	}
}
