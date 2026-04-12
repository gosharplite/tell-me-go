// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
	if p.safePaths[0] == filepath.Join(os.TempDir(), "hacked") {
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
