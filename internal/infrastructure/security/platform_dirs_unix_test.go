// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

//go:build !windows

package security

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestIsSystemDirectory_Unix(t *testing.T) {
	p := newPathPolicyForTest(t)

	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{"/etc is forbidden", "/etc", true},
		{"/etc/passwd is forbidden", "/etc/passwd", true},
		{"/usr/bin is forbidden", "/usr/bin", true},
		{"/usr/sbin is forbidden", "/usr/sbin", true},
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
			abs, err := filepath.Abs(tt.path)
			if err != nil {
				t.Fatalf("filepath.Abs(%s) failed: %v", tt.path, err)
			}
			err = p.isSystemDirectory(abs)
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
