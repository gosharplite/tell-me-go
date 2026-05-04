// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build windows

package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSystemDirectory_Windows(t *testing.T) {
	p := newPathPolicyForTest(t)

	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		t.Skip("SystemRoot not set; skipping Windows system-dir tests")
	}

	absSys, err := filepath.Abs(sysRoot)
	if err != nil {
		t.Fatalf("filepath.Abs(SystemRoot) failed: %v", err)
	}

	absSys32, err := filepath.Abs(filepath.Join(strings.ToUpper(sysRoot), "System32"))
	if err != nil {
		t.Fatalf("filepath.Abs(SystemRoot\\System32) failed: %v", err)
	}

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"SystemRoot is forbidden", absSys, true},
		{"SystemRoot subdir is forbidden (case-insensitive)", absSys32, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := p.isSystemDirectory(tt.path)
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
}
