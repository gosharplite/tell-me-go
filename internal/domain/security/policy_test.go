// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "testing"

func TestDefaultPolicy(t *testing.T) {
	t.Parallel()
	p := DefaultPolicy()
	if p == nil {
		t.Fatal("DefaultPolicy() returned nil")
	}

	expectedCommands := []string{"ls", "git", "go", "read_files", "execute_command"}
	for _, cmd := range expectedCommands {
		if !p.AllowedCommands[cmd] {
			t.Errorf("expected command %s to be allowed", cmd)
		}
	}

	if len(p.ForbiddenPatterns) == 0 {
		t.Error("expected ForbiddenPatterns to be populated")
	}

	if len(p.AllowedCommandPrefixes) != 1 || p.AllowedCommandPrefixes[0] != "mcp_" {
		t.Errorf("expected AllowedCommandPrefixes to be [\"mcp_\"], got %v", p.AllowedCommandPrefixes)
	}
}

func TestPolicy_IsCommandAllowed(t *testing.T) {
	t.Parallel()
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
		{"mcp_prefix_not_exact", "mcp_github_run_secret_scanning", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := p.IsCommandAllowed(tt.cmd); got != tt.want {
				t.Errorf("IsCommandAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestPolicy_isAutoApprovable(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
			if got := p.isAutoApprovable(tt.cmd); got != tt.want {
				t.Errorf("isAutoApprovable(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestPolicy_IsToolAllowed(t *testing.T) {
	t.Parallel()
	p := DefaultPolicy()
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"exact_command", "ls", true},
		{"exact_tool", "read_files", true},
		{"exact_execute", "execute_command", true},
		{"mcp_github_tool", "mcp_github_run_secret_scanning", true},
		{"mcp_github_list", "mcp_github_list_issues", true},
		{"mcp_truncated_form", "mcp_github_some_very_long_tool_name_abcdef12", true},
		{"unknown_non_mcp", "unknown_tool", false},
		{"mcp_without_underscore", "mcp", false},
		{"mcp_uppercase", "MCP_github_list_issues", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := p.IsToolAllowed(tt.cmd); got != tt.want {
				t.Errorf("IsToolAllowed(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}
