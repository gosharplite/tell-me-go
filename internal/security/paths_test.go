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
	p := NewPathPolicy()
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
	p := NewPathPolicy()
	tmpFile := filepath.Join(t.TempDir(), "paths.json")
	p.SetConfigFile(tmpFile, true)

	p.RegisterPath("/tmp/test", true)
	if err := p.SavePaths(context.Background(), true); err != nil {
		t.Fatalf("SavePaths failed: %v", err)
	}

	p2 := NewPathPolicy()
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
