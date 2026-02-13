// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "testing"

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p == nil {
		t.Fatal("DefaultPolicy() returned nil")
	}

	expectedCommands := []string{"ls", "git", "go", "read_file", "execute_command"}
	for _, cmd := range expectedCommands {
		if !p.AllowedCommands[cmd] {
			t.Errorf("expected command %s to be allowed", cmd)
		}
	}

	if len(p.ForbiddenPatterns) == 0 {
		t.Error("expected ForbiddenPatterns to be populated")
	}
}

func TestPolicy_IsCommandAllowed(t *testing.T) {
	p := DefaultPolicy()
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"ls", "ls", true},
		{"git", "git", true},
		{"rm", "rm", true},
		{"forbidden", "forbidden_cmd", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.IsCommandAllowed(tt.cmd); got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestPolicy_isAutoApprovable(t *testing.T) {
	p := DefaultPolicy()
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"ls", "ls", true},
		{"grep", "grep", true},
		{"run_benchmark", "run_benchmark", true},
		{"rm", "rm", false},
		{"execute_command", "execute_command", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.isAutoApprovable(tt.cmd); got != tt.want {
				t.Errorf("isAutoApprovable(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
