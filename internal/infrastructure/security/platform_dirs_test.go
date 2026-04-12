// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestIsSystemDirectory_PlatformAware(t *testing.T) {
	p := newPathPolicy(nil)
	cwd, _ := os.Getwd()

	// Test CWD exemption
	if err := p.isSystemDirectory(cwd); err != nil {
		t.Errorf("expected CWD to be allowed, got error: %v", err)
	}

	// Test Temp dir exemption
	tempDir := os.TempDir()
	// We need to be careful here because resolvedTempDir might have symlinks resolved
	// but the check in isSystemDirectory uses strings.HasPrefix with p.resolvedTempDir
	// and p.resolvedTempDir has a trailing separator.
	
	// Since we can't easily guess how it was initialized in newPathPolicy, 
	// let's just check that SOME temp file is allowed.
	tempFile := filepath.Join(tempDir, "test.txt")
	absTempFile, _ := filepath.Abs(tempFile)
	if err := p.isSystemDirectory(absTempFile); err != nil {
		t.Errorf("expected temp file to be allowed, got error: %v", err)
	}

	if runtime.GOOS != "windows" {
		// Unix specific tests
		tests := []struct {
			name    string
			path    string
			wantErr bool
		}{
			{"/etc is forbidden", "/etc", true},
			{"/etc/passwd is forbidden", "/etc/passwd", true},
			{"/usr is forbidden", "/usr", true},
			{"/usr/bin is forbidden", "/usr/bin", true},
			{"/bin is forbidden", "/bin", true},
			{"/sbin is forbidden", "/sbin", true},
			{"/var is forbidden", "/var", true},
			{"/root is forbidden", "/root", true},
			{"/boot is forbidden", "/boot", true},
			{"/dev is forbidden", "/dev", true},
			{"/proc is forbidden", "/proc", true},
			{"/sys is forbidden", "/sys", true},
			{"/home is allowed (not in sensitive list)", "/home", false},
			{"Random path is allowed", "/something/random", false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				abs, _ := filepath.Abs(tt.path)
				err := p.isSystemDirectory(abs)
				if (err != nil) != tt.wantErr {
					t.Errorf("isSystemDirectory(%s) error = %v, wantErr %v", tt.path, err, tt.wantErr)
				}
				if tt.wantErr && err != nil {
					if !strings.Contains(err.Error(), "forbidden") {
						t.Errorf("expected forbidden error message, got: %v", err)
					}
				}
			})
		}
	} else {
		// Windows specific tests (running on Windows)
		// We can test at least SystemRoot if it's set
		sysRoot := os.Getenv("SystemRoot")
		if sysRoot != "" {
			absSys, _ := filepath.Abs(sysRoot)
			t.Run("SystemRoot is forbidden", func(t *testing.T) {
				if err := p.isSystemDirectory(absSys); err == nil {
					t.Errorf("expected SystemRoot (%s) to be forbidden", absSys)
				}
			})
			
			t.Run("SystemRoot subdir is forbidden (case-insensitive)", func(t *testing.T) {
				path := filepath.Join(strings.ToUpper(sysRoot), "System32")
				abs, _ := filepath.Abs(path)
				if err := p.isSystemDirectory(abs); err == nil {
					t.Errorf("expected %s to be forbidden", abs)
				}
			})
		}
	}
}
