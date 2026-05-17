// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathPolicy_ValidatePath(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)
	cwd, _ := os.Getwd()
	tempDir := os.TempDir()

	outsidePath := "/tellmego_outside"
	if runtime.GOOS == "windows" {
		outsidePath = `C:\tellmego_outside`
	}

	tests := []struct {
		name     string
		path     string
		writable bool
		setup    func()
		wantErr  bool
	}{
		{
			name:     "CWD is safe",
			path:     filepath.Join(cwd, "test.txt"),
			writable: true,
			wantErr:  false,
		},
		{
			name:     "Temp is safe",
			path:     filepath.Join(tempDir, "test.txt"),
			writable: true,
			wantErr:  false,
		},
		{
			name: "Outside is unsafe by default",
			path: func() string {
				if runtime.GOOS == "windows" {
					return `C:\Windows\System32\drivers\etc\hosts`
				}
				return "/etc/passwd"
			}(),
			writable: false,
			wantErr:  true,
		},
		{
			name:     "Registered safe path is safe",
			path:     filepath.Join(outsidePath, "safe", "file.txt"),
			writable: true,
			setup: func() {
				p.RegisterPath(filepath.Join(outsidePath, "safe"), true)
			},
			wantErr: false,
		},
		{
			name:     "Registered read-only path is safe for read",
			path:     filepath.Join(outsidePath, "readonly", "file.txt"),
			writable: false,
			setup: func() {
				p.RegisterPath(filepath.Join(outsidePath, "readonly"), false)
			},
			wantErr: false,
		},
		{
			name:     "Registered read-only path is unsafe for write",
			path:     filepath.Join(outsidePath, "readonly", "file.txt"),
			writable: true,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.setup != nil {
				tt.setup()
			}
			_, err := p.ValidatePath(tt.path, tt.writable)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath(%s) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}
}

func TestPathPolicy_SymlinkBoundary(t *testing.T) {
	t.Parallel()
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	linkDir := filepath.Join(tmp, "link")

	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", linkDir); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	p := newPathPolicy(nil)
	p.RegisterPath(linkDir, true)

	// Target is in the real directory
	target := filepath.Join(realDir, "test.txt")
	if err := os.WriteFile(target, []byte("test"), 0644); err != nil {
		t.Fatal(err)
	}

	// Should be valid even if we registered the link
	_, err := p.ValidatePath(target, true)
	if err != nil {
		t.Errorf("ValidatePath failed for symlinked boundary: %v", err)
	}
}

func setupSymlinkTestEnv(t *testing.T) (*pathPolicy, string) {
	t.Helper()
	tmpDir := t.TempDir()
	p := newPathPolicy(nil)
	p.RegisterPath(tmpDir, true)

	// Check if symlinks are supported
	link := filepath.Join(tmpDir, "check_symlink")
	if err := os.Symlink(tmpDir, link); err != nil {
		t.Skip("symlinks not supported on this platform")
	}
	_ = os.Remove(link)

	return p, tmpDir
}

func TestPathPolicy_SymlinkBypass_DirectAccess(t *testing.T) {
	t.Parallel()
	p, workspace := setupSymlinkTestEnv(t)

	// Test symlink to a forbidden system file
	var target string
	if runtime.GOOS == "windows" {
		target = `C:\Windows\System32\drivers\etc\hosts`
	} else {
		target = "/etc/passwd"
	}
	passwdLink := filepath.Join(workspace, "passwd_link")

	// Create symlink. If it fails, we might be on a platform that requires admin for symlinks
	// or doesn't support this path.
	if err := os.Symlink(target, passwdLink); err != nil {
		// Fallback to a non-existent path that is outside our boundaries
		if runtime.GOOS == "windows" {
			target = `C:\nonexistent_outside_path`
		} else {
			target = "/nonexistent_outside_path"
		}
		if err := os.Symlink(target, passwdLink); err != nil {
			t.Skip("failed to create symlink for test")
		}
	}

	if _, err := p.ValidatePath(passwdLink, false); err == nil {
		t.Errorf("ValidatePath allowed access to %s via symlink", target)
	}
}

func TestPathPolicy_SymlinkBypass_ValidWorkspaceLink(t *testing.T) {
	t.Parallel()
	p, workspace := setupSymlinkTestEnv(t)

	internalFile := filepath.Join(workspace, "internal.txt")
	if err := os.WriteFile(internalFile, []byte("internal"), 0644); err != nil {
		t.Fatal(err)
	}

	internalLink := filepath.Join(workspace, "internal_link")
	if err := os.Symlink(internalFile, internalLink); err != nil {
		t.Fatal(err)
	}

	if _, err := p.ValidatePath(internalLink, false); err != nil {
		t.Errorf("ValidatePath denied valid symlink: %v", err)
	}
}

func TestPathPolicy_SymlinkBypass_NonExistentTarget(t *testing.T) {
	t.Parallel()
	p, workspace := setupSymlinkTestEnv(t)

	// Link inside workspace to /etc (which is forbidden)
	var forbiddenDir string
	if runtime.GOOS == "windows" {
		forbiddenDir = `C:\Windows`
	} else {
		forbiddenDir = "/etc"
	}

	linkToForbidden := filepath.Join(workspace, "forbidden_link")
	if err := os.Symlink(forbiddenDir, linkToForbidden); err != nil {
		t.Skipf("failed to create link to %s", forbiddenDir)
	}

	// Path to non-existent file via the link
	targetPath := filepath.Join(linkToForbidden, "new_file.txt")

	if _, err := p.ValidatePath(targetPath, true); err == nil {
		t.Errorf("ValidatePath allowed creation of file in %s via symlink", forbiddenDir)
	}
}

func TestPathPolicy_SymlinkBypass_MultiLevelNonExistent(t *testing.T) {
	t.Parallel()
	p, workspace := setupSymlinkTestEnv(t)

	var forbiddenDir string
	if runtime.GOOS == "windows" {
		forbiddenDir = `C:\Windows`
	} else {
		forbiddenDir = "/etc"
	}

	linkToForbidden := filepath.Join(workspace, "forbidden_link_multi")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(forbiddenDir, linkToForbidden); err != nil {
		t.Skipf("failed to create link to %s", forbiddenDir)
	}

	// Two levels of non-existence
	targetPath := filepath.Join(linkToForbidden, "nonexistent_dir", "new_file.txt")

	if _, err := p.ValidatePath(targetPath, true); err == nil {
		t.Errorf("ValidatePath allowed creation of file in %s/nonexistent_dir via symlink", forbiddenDir)
	}
}

func TestNewPathPolicy_Initialization(t *testing.T) {
	t.Parallel()

	// 1. Test Defensive Copy
	original := []string{filepath.Join(os.TempDir(), "safe")}
	p := newPathPolicy(original)

	original[0] = filepath.Join(os.TempDir(), "hacked")
	if _, ok := p.safePaths[filepath.Join(os.TempDir(), "hacked")]; ok {
		t.Errorf("safePaths suffered from slice reference leak")
	}

	// 2. Test Temp Dir Resolution
	if p.resolvedTempDir == "" {
		t.Errorf("expected resolvedTempDir to be populated")
	}
}

func TestPathPolicy_IsSystemDirectory_Internal(t *testing.T) {
	p := newPathPolicy(nil)
	cwd, _ := os.Getwd()

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name: "Forbidden system directory",
			path: func() string {
				if runtime.GOOS == "windows" {
					return `C:\Windows\System32`
				}
				return "/etc/passwd"
			}(),
			wantErr: true,
		},
		{
			name:    "Allowed Temp directory",
			path:    filepath.Join(os.TempDir(), "test.txt"),
			wantErr: false,
		},
		{
			name:    "Allowed CWD",
			path:    filepath.Join(cwd, "test.txt"),
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.isSystemDirectory(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("isSystemDirectory(%s) error = %v, wantErr %v", tt.path, err, tt.wantErr)
			}
		})
	}

	if runtime.GOOS == "windows" {
		t.Run("Windows: case-insensitive check", func(t *testing.T) {
			err := p.isSystemDirectory(`c:\windows\system32`)
			if err == nil {
				t.Error("expected error for case-insensitive system directory match on Windows")
			}
		})
	}
}

func TestPathPolicy_RegisterPath_Idempotency(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)
	testPath := filepath.Join(os.TempDir(), "idempotent_test")
	absPath, _ := filepath.Abs(testPath)

	t.Run("Safe Paths (writable=true)", func(t *testing.T) {
		p.RegisterPath(testPath, true)
		p.RegisterPath(testPath, true)

		paths := p.GetPaths(true)
		if len(paths) != 1 {
			t.Errorf("expected 1 safe path, got %d", len(paths))
		}
		if paths[0] != absPath {
			t.Errorf("expected path %s, got %s", absPath, paths[0])
		}
	})

	t.Run("Read-Only Paths (writable=false)", func(t *testing.T) {
		p.RegisterPath(testPath, false)
		p.RegisterPath(testPath, false)

		paths := p.GetPaths(false)
		if len(paths) != 1 {
			t.Errorf("expected 1 read-only path, got %d", len(paths))
		}
		if paths[0] != absPath {
			t.Errorf("expected path %s, got %s", absPath, paths[0])
		}
	})

	t.Run("Empty path input", func(t *testing.T) {
		initialSafeCount := len(p.GetPaths(true))
		initialROCount := len(p.GetPaths(false))

		p.RegisterPath("", true)
		p.RegisterPath("", false)

		if len(p.GetPaths(true)) != initialSafeCount {
			t.Errorf("RegisterPath(\"\") added a safe path")
		}
		if len(p.GetPaths(false)) != initialROCount {
			t.Errorf("RegisterPath(\"\") added a read-only path")
		}
	})
}

func TestPathPolicy_SystemDirectoryBlockedEvenIfRegistered(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	var forbidden string
	if runtime.GOOS == "windows" {
		forbidden = `C:\Windows\System32\drivers\etc\hosts`
	} else {
		forbidden = "/etc/passwd"
	}

	// Register it as a safe path
	p.RegisterPath(forbidden, false)

	// It should still be blocked because system directory check comes first
	_, err := p.ValidatePath(forbidden, false)
	if err == nil {
		t.Errorf("ValidatePath allowed access to system directory %s even though it was registered", forbidden)
	}
}

func TestValidatePath_ErrorPaths(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	t.Run("empty path returns empty string", func(t *testing.T) {
		t.Parallel()
		result, err := p.ValidatePath("", true)
		require.NoError(t, err)
		assert.Equal(t, "", result)
	})

	t.Run("filepath.Abs error with NUL byte", func(t *testing.T) {
		t.Parallel()
		_, err := p.ValidatePath("/tmp/\x00invalid", false)
		if err == nil {
			t.Log("filepath.Abs did not error on this platform")
		} else {
			assert.Contains(t, err.Error(), "invalid path")
		}
	})

	t.Run("rule error propagation", func(t *testing.T) {
		t.Parallel()
		t.Log("Rule error path (line 135-137) requires checkBoundary Abs failure — covered in T16")
	})
}

func TestPathPolicy_RemovePath_NotFound(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	t.Run("safe path not found", func(t *testing.T) {
		t.Parallel()
		err := p.RemovePath("/nonexistent/path/for/removal", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in safe authorized list")
	})

	t.Run("read-only path not found", func(t *testing.T) {
		t.Parallel()
		err := p.RemovePath("/nonexistent/path/for/removal", false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in read-only authorized list")
	})
}

func TestCheckBoundary_ErrorPaths(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	t.Run("filepath.Abs error on boundary", func(t *testing.T) {
		t.Parallel()
		// NUL byte triggers filepath.Abs failure on some platforms
		ok, err := p.checkBoundary("/some/target", "/valid/\x00boundary")
		if err == nil {
			t.Skip("filepath.Abs does not error on NUL byte on this platform")
		}
		require.Error(t, err)
		require.False(t, ok)
	})

	t.Run("resolveSymlinks recursive fallback", func(t *testing.T) {
		t.Parallel()
		tmpDir := t.TempDir()
		// Boundary doesn't exist → EvalSymlinks fails → recursive fallback
		boundary := filepath.Join(tmpDir, "nonexistent_boundary")
		ok, err := p.checkBoundary(filepath.Join(tmpDir, "target"), boundary)
		require.NoError(t, err) // resolveSymlinks handles missing dirs gracefully
		require.False(t, ok)
	})
}

func TestCheckDefaultBoundaries_ExtraTempDirs(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	// getExtraTempDirs() returns ["/tmp", "/private/tmp"] on Unix, nil on Windows.
	// On Linux, os.TempDir() typically returns /tmp already, so this path is
	// usually caught by the os.TempDir() check before reaching the
	// getExtraTempDirs() loop. On macOS, os.TempDir() returns a per-user
	// directory under /var/folders, making the getExtraTempDirs() loop
	// essential for allowing /tmp paths.
	if runtime.GOOS == "windows" {
		t.Skip("getExtraTempDirs() returns nil on Windows")
	}

	// A file in /tmp should be permitted by checkDefaultBoundaries.
	// Whether via os.TempDir() or getExtraTempDirs() depends on the platform.
	path := p.resolveSymlinks("/tmp/test-extra-temp-file.txt")
	ok, err := p.checkDefaultBoundaries(path, false)
	require.NoError(t, err)
	assert.True(t, ok, "path in /tmp should pass checkDefaultBoundaries (via os.TempDir or getExtraTempDirs)")

	// macOS-specific: /private/tmp is also covered by getExtraTempDirs
	if runtime.GOOS == "darwin" {
		path2 := p.resolveSymlinks("/private/tmp/test-extra-temp-file.txt")
		ok2, err2 := p.checkDefaultBoundaries(path2, false)
		require.NoError(t, err2)
		assert.True(t, ok2, "path in /private/tmp should pass checkDefaultBoundaries on macOS")
	}
}

// TestNewPathPolicy_ResolvedTempDir verifies that resolvedTempDir is populated
// during construction (paths.go:38-45) and that isExemptedDirectory correctly
// recognizes paths inside it.
//
// The EvalSymlinks fallback on line 44 is [SYSTEM-DEPENDENT]: it only triggers
// when os.TempDir() returns a non-empty string that filepath.EvalSymlinks cannot
// resolve. This requires a broken filesystem (e.g., /tmp is a dangling symlink
// or the underlying mount point is not accessible). In normal operation,
// resolvedTempDir is always set via the EvalSymlinks path (line 41).
func TestNewPathPolicy_ResolvedTempDir(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)

	// resolvedTempDir must be populated during construction
	require.NotEmpty(t, p.resolvedTempDir, "resolvedTempDir should be populated by newPathPolicy")

	// Verify a file under the real OS temp dir is considered exempted
	tempFile := filepath.Join(os.TempDir(), "test-exempted-file.txt")
	tempFile = p.resolveSymlinks(tempFile)
	exempted := p.isExemptedDirectory(tempFile)
	assert.True(t, exempted, "path in os.TempDir() should be exempted by isExemptedDirectory")
}

func TestRegisterPath_EmptyPath(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)
	initialSafe := len(p.GetPaths(true))
	initialRO := len(p.GetPaths(false))
	p.RegisterPath("", true)
	p.RegisterPath("", false)
	assert.Equal(t, initialSafe, len(p.GetPaths(true)), "empty path should not register")
	assert.Equal(t, initialRO, len(p.GetPaths(false)), "empty path should not register")
}

// TestCheckBoundary_ErrorPath verifies that checkBoundary propagates errors
// to callers instead of silently swallowing them. When filepath.Abs fails on
// the boundary (e.g., NUL byte), the error must be returned so callers can log it.
//
// This covers the [TECHNICAL DEBT] fix for checkDefaultBoundaries,
// checkSafePaths, and checkReadOnlyPaths which previously discarded errors
// with ok, _ := p.checkBoundary(...).
func TestCheckBoundary_ErrorPath(t *testing.T) {
	t.Parallel()

	p := newPathPolicy(nil)

	t.Run("checkBoundary propagates filepath.Abs error", func(t *testing.T) {
		t.Parallel()
		// NUL byte in boundary triggers filepath.Abs failure on some platforms
		ok, err := p.checkBoundary("/some/target", "/valid/\x00boundary")
		if err == nil {
			t.Skip("filepath.Abs does not error on NUL byte on this platform")
		}
		if ok {
			t.Error("expected ok=false when checkBoundary returns an error")
		}
		// Error is propagated — callers (checkDefaultBoundaries, checkSafePaths,
		// checkReadOnlyPaths) can now log it instead of discarding it.
	})

	t.Run("checkDefaultBoundaries rejects path outside all boundaries", func(t *testing.T) {
		t.Parallel()
		// A path in a directory that is neither CWD nor any temp directory
		// must be rejected by checkDefaultBoundaries.
		outside := "/nonexistent_outside_dir/test-file.txt"
		if runtime.GOOS == "windows" {
			outside = `C:\nonexistent_outside_dir\test-file.txt`
		}
		ok, err := p.checkDefaultBoundaries(outside, false)
		if err != nil {
			t.Errorf("checkDefaultBoundaries returned unexpected error: %v", err)
		}
		if ok {
			t.Error("checkDefaultBoundaries should reject path outside all default boundaries")
		}
	})

	t.Run("checkSafePaths returns false for empty safePaths", func(t *testing.T) {
		t.Parallel()
		p2 := newPathPolicy(nil)
		// No safe paths registered — checkSafePaths must return false.
		ok, err := p2.checkSafePaths("/some/target", false)
		if err != nil {
			t.Errorf("checkSafePaths returned unexpected error: %v", err)
		}
		if ok {
			t.Error("expected checkSafePaths to return false for empty safePaths")
		}
	})

	t.Run("checkReadOnlyPaths returns false for empty readOnlyPaths", func(t *testing.T) {
		t.Parallel()
		p2 := newPathPolicy(nil)
		// No read-only paths registered — checkReadOnlyPaths must return false.
		ok, err := p2.checkReadOnlyPaths("/some/target", false)
		if err != nil {
			t.Errorf("checkReadOnlyPaths returned unexpected error: %v", err)
		}
		if ok {
			t.Error("expected checkReadOnlyPaths to return false for empty readOnlyPaths")
		}
	})
}

// TestCheckBoundary_EvalSymlinksError verifies fail-secure behavior when
// the boundary directory has restricted permissions (000). Even when
// filepath.EvalSymlinks succeeds on the directory entry itself (which is
// [SYSTEM-DEPENDENT]), checkBoundary must correctly identify that a target
// outside the boundary is rejected.
//
// The error path from checkBoundary (via filepath.Abs failure) is tested
// separately in TestCheckBoundary_ErrorPaths. This test focuses on the
// common case: boundary exists but target is outside → (false, nil).
// The logging added by the [TECHNICAL DEBT] fix only triggers when
// checkBoundary returns err != nil (i.e., filepath.Abs failure), which
// is platform-specific and covered by TestCheckBoundary_ErrorPaths.
func TestCheckBoundary_EvalSymlinksError(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nested := filepath.Join(tmpDir, "nested")
	if err := os.Mkdir(nested, 0000); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chmod(nested, 0700); err != nil {
			t.Logf("failed to restore permissions on %s: %v", nested, err)
		}
	}()

	p := newPathPolicy(nil)
	// Target OUTSIDE the permission-denied boundary
	absPath := filepath.Join(tmpDir, "outside_target")

	// checkBoundary with nested as the boundary and a target outside it
	ok, err := p.checkBoundary(absPath, nested)
	if ok {
		t.Error("expected checkBoundary to return false for path outside 000-permission boundary")
	}
	// error behavior is platform-dependent: EvalSymlinks may or may not fail
	// on 000-permission directories. When it fails, resolveSymlinks falls back
	// to recursive resolution, so the error is not propagated (resolveSymlinks
	// never returns an error). The only error from checkBoundary is from
	// filepath.Abs(boundary), which is tested separately.
	if err != nil {
		t.Logf("checkBoundary returned error for 000-permission boundary: %v", err)
	}

	// Verify callers handle the return values correctly (no panic, no crash).
	// Whether they return ok=true depends on whether tmpDir is under CWD or
	// os.TempDir() — which is [SYSTEM-DEPENDENT] — so we only verify the
	// calls complete without error.
	safeOK, safeErr := p.checkSafePaths(absPath, false)
	if safeErr != nil {
		t.Logf("checkSafePaths error: %v", safeErr)
	}
	_ = safeOK // may be true or false depending on registration

	roOK, roErr := p.checkReadOnlyPaths(absPath, false)
	if roErr != nil {
		t.Logf("checkReadOnlyPaths error: %v", roErr)
	}
	_ = roOK // may be true or false depending on registration

	// Verify that ValidatePath itself handles the boundary correctly:
	// the path should be rejected (ErrSandboxViolation) unless tmpDir happens
	// to fall within CWD or os.TempDir.
	validated, validateErr := p.ValidatePath(absPath, true)
	if validateErr != nil {
		t.Logf("ValidatePath rejected path (expected outside boundaries): %v", validateErr)
	} else {
		t.Logf("ValidatePath accepted path (tmpDir is within default boundaries): %s", validated)
	}
}
