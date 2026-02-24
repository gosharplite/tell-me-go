// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPathPolicy_ValidatePath(t *testing.T) {
	p := newPathPolicy()
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

func TestPathPolicy_Persistence(t *testing.T) {
	p := newPathPolicy()
	tmpFile := filepath.Join(t.TempDir(), "paths.json")
	p.SetConfigFile(tmpFile, true)

	p.RegisterPath("/tmp/test", true)
	if err := p.SavePaths(context.Background(), true); err != nil {
		t.Fatalf("SavePaths failed: %v", err)
	}

	p2 := newPathPolicy()
	p2.SetConfigFile(tmpFile, true)
	if err := p2.LoadPaths(true); err != nil {
		t.Fatalf("LoadPaths failed: %v", err)
	}

	paths := p2.GetPaths(true)
	found := false
	absTest, _ := filepath.Abs("/tmp/test")
	for _, path := range paths {
		if path == absTest {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected path %s not found in loaded paths %v", absTest, paths)
	}
}

func TestPathPolicy_SymlinkBoundary(t *testing.T) {
	tmp := t.TempDir()
	realDir := filepath.Join(tmp, "real")
	linkDir := filepath.Join(tmp, "link")

	if err := os.Mkdir(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", linkDir); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	p := newPathPolicy()
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

func TestPathPolicy_SymlinkBypass(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create a forbidden file outside the workspace
	forbiddenDir := t.TempDir()
	forbiddenFile := filepath.Join(forbiddenDir, "forbidden.txt")
	if err := os.WriteFile(forbiddenFile, []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}

	// Setup path policy with tmpDir as safe boundary
	p := newPathPolicy()
	p.RegisterPath(tmpDir, true)

	// Create a symlink INSIDE tmpDir pointing to the forbidden file OUTSIDE
	linkPath := filepath.Join(tmpDir, "malicious_link")
	if err := os.Symlink(forbiddenFile, linkPath); err != nil {
		t.Skip("symlinks not supported on this platform")
	}

	// 1. Test Symlink Bypass (Should be denied)
	t.Run("Symlink bypass to /etc/passwd", func(t *testing.T) {
		passwdLink := filepath.Join(tmpDir, "passwd_link")
		if err := os.Symlink("/etc/passwd", passwdLink); err != nil {
			t.Skip("symlinks not supported or failed to create")
		}
		_, err := p.ValidatePath(passwdLink, false)
		if err == nil {
			t.Error("ValidatePath allowed access to /etc/passwd via symlink")
		}
	})

	// 2. Test Valid Symlink (Should be allowed)
	t.Run("Valid symlink within workspace", func(t *testing.T) {
		internalFile := filepath.Join(tmpDir, "internal.txt")
		if err := os.WriteFile(internalFile, []byte("internal"), 0644); err != nil {
			t.Fatal(err)
		}
		
		internalLink := filepath.Join(tmpDir, "internal_link")
		if err := os.Symlink(internalFile, internalLink); err != nil {
			t.Fatal(err)
		}

		_, err := p.ValidatePath(internalLink, false)
		if err != nil {
			t.Errorf("ValidatePath denied valid symlink: %v", err)
		}
	})
	
	// 3. Test non-existent file with symlink in path
	t.Run("Symlink in path to non-existent file", func(t *testing.T) {
		// Link inside tmpDir to /etc (which is forbidden)
		linkToEtc := filepath.Join(tmpDir, "etc_link")
		if err := os.Symlink("/etc", linkToEtc); err != nil {
			t.Skip("symlinks not supported or failed to create")
		}
		
		// Path to non-existent file via the link
		targetPath := filepath.Join(linkToEtc, "new_file.txt")
		
		_, err := p.ValidatePath(targetPath, true)
		if err == nil {
			t.Error("ValidatePath allowed creation of file in /etc via symlink")
		}
	})

	// 4. Test multi-level non-existent path with symlink
	t.Run("Multi-level non-existent path with symlink", func(t *testing.T) {
		linkToEtc := filepath.Join(tmpDir, "etc_link_multi")
		if err := os.Symlink("/etc", linkToEtc); err != nil {
			t.Skip("symlinks not supported or failed to create")
		}

		// Two levels of non-existence
		targetPath := filepath.Join(linkToEtc, "nonexistent_dir", "new_file.txt")

		_, err := p.ValidatePath(targetPath, true)
		if err == nil {
			t.Error("ValidatePath allowed creation of file in /etc/nonexistent_dir via symlink")
		}
	})
}
