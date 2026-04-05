// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathPolicy_ValidatePath(t *testing.T) {
	t.Parallel()
	p := newPathPolicy(nil)
	cwd, _ := os.Getwd()
	tempDir := os.TempDir()

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
			name:     "Outside is unsafe by default",
			path:     "/etc/passwd",
			writable: false,
			wantErr:  true,
		},
		{
			name:     "Registered safe path is safe",
			path:     "/nonexistent/safe/file.txt",
			writable: true,
			setup: func() {
				p.RegisterPath("/nonexistent/safe", true)
			},
			wantErr: false,
		},
		{
			name:     "Registered read-only path is safe for read",
			path:     "/nonexistent/readonly/file.txt",
			writable: false,
			setup: func() {
				p.RegisterPath("/nonexistent/readonly", false)
			},
			wantErr: false,
		},
		{
			name:     "Registered read-only path is unsafe for write",
			path:     "/nonexistent/readonly/file.txt",
			writable: true,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.setup != nil {
				tt.setup()
			}
			_, err := p.ValidatePath(tt.path, tt.writable)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePath() error = %v, wantErr %v", err, tt.wantErr)
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
	passwdLink := filepath.Join(workspace, "passwd_link")
	target := "/etc/passwd"

	// Create symlink. If it fails, we might be on a platform that requires admin for symlinks
	// or doesn't support this path.
	if err := os.Symlink(target, passwdLink); err != nil {
		// Fallback to a non-existent path that is outside our boundaries
		target = "/nonexistent_outside_path"
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
	linkToEtc := filepath.Join(workspace, "etc_link")
	if err := os.Symlink("/etc", linkToEtc); err != nil {
		t.Skip("failed to create link to /etc")
	}

	// Path to non-existent file via the link
	targetPath := filepath.Join(linkToEtc, "new_file.txt")

	if _, err := p.ValidatePath(targetPath, true); err == nil {
		t.Error("ValidatePath allowed creation of file in /etc via symlink")
	}
}

func TestPathPolicy_SymlinkBypass_MultiLevelNonExistent(t *testing.T) {
	t.Parallel()
	p, workspace := setupSymlinkTestEnv(t)

	linkToEtc := filepath.Join(workspace, "etc_link_multi")
	if err := os.Symlink("/etc", linkToEtc); err != nil {
		t.Skip("failed to create link to /etc")
	}

	// Two levels of non-existence
	targetPath := filepath.Join(linkToEtc, "nonexistent_dir", "new_file.txt")

	if _, err := p.ValidatePath(targetPath, true); err == nil {
		t.Error("ValidatePath allowed creation of file in /etc/nonexistent_dir via symlink")
	}
}

func TestNewPathPolicy_Initialization(t *testing.T) {
	t.Parallel()

	// 1. Test Defensive Copy
	original := []string{"/tmp/safe"}
	p := newPathPolicy(original)

	original[0] = "/tmp/hacked"
	if p.safePaths[0] == "/tmp/hacked" {
		t.Errorf("safePaths suffered from slice reference leak")
	}

	// 2. Test Temp Dir Resolution
	if p.resolvedTempDir == "" {
		t.Errorf("expected resolvedTempDir to be populated")
	}
}
