// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "testing"

func TestSafetyService_IsCommandSafe(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name     string
		cmd      string
		wantSafe bool
		wantMsg  string
	}{
		{"allowed and auto-approvable", "ls", true, ""},
		{"allowed but not auto-approvable", "rm", false, "command requires manual confirmation"},
		{"not allowed", "unknown_cmd", false, "command not allowed by policy"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotSafe, gotMsg := s.IsCommandSafe(tt.cmd)
			if gotSafe != tt.wantSafe {
				t.Errorf("IsCommandSafe(%q) gotSafe = %v, want %v", tt.cmd, gotSafe, tt.wantSafe)
			}
			if gotMsg != tt.wantMsg {
				t.Errorf("IsCommandSafe(%q) gotMsg = %v, want %v", tt.cmd, gotMsg, tt.wantMsg)
			}
		})
	}
}

func TestSafetyService_HasForbiddenOperators(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name  string
		parts []string
		want  bool
		desc  string
	}{
		{"clean command", []string{"ls", "-la"}, false, ""},
		{"logical AND", []string{"ls", "&&", "whoami"}, true, "logical AND"},
		{"logical OR", []string{"ls", "||", "whoami"}, true, "logical OR"},
		{"command separator", []string{"ls", ";", "whoami"}, true, "command separator"},
		{"pipe", []string{"ls", "|", "grep", "foo"}, true, "pipe"},
		{"output redirection", []string{"ls", ">", "out.txt"}, true, "output redirection"},
		{"append redirection", []string{"ls", ">>", "out.txt"}, true, "append redirection"},
		{"input redirection", []string{"grep", "foo", "<", "in.txt"}, true, "input redirection"},
		{"background execution", []string{"ls", "&"}, true, "background execution"},
		{"error redirection", []string{"ls", "2>", "err.txt"}, true, "error redirection"},
		{"combined redirection", []string{"ls", "&>", "all.txt"}, true, "combined redirection"},
		{"combined pipe", []string{"ls", "|&", "grep", "foo"}, true, "combined pipe"},
		{"output redirection 1>", []string{"ls", "1>", "out.txt"}, true, "output redirection"},
		{"append redirection 1>>", []string{"ls", "1>>", "out.txt"}, true, "append redirection"},
		{"error append redirection 2>>", []string{"ls", "2>>", "err.txt"}, true, "error append redirection"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, gotDesc := s.HasForbiddenOperators(tt.parts)
			if got != tt.want {
				t.Errorf("HasForbiddenOperators(%v) got = %v, want %v", tt.parts, got, tt.want)
			}
			if tt.want && gotDesc != tt.desc {
				t.Errorf("HasForbiddenOperators(%v) desc = %v, want %v", tt.parts, gotDesc, tt.desc)
			}
		})
	}
}

func TestSafetyService_HasUnsafeInterpolation(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name string
		part string
		want bool
	}{
		{"clean part", "ls", false},
		{"variable expansion", "$HOME", true},
		{"command substitution", "`whoami`", true},
		{"mixed part", "foo$bar", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := s.HasUnsafeInterpolation(tt.part); got != tt.want {
				t.Errorf("HasUnsafeInterpolation(%q) = %v, want %v", tt.part, got, tt.want)
			}
		})
	}
}

func TestSafetyService_HasForbiddenCharsInCommand(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{"clean command", "ls", false},
		{"semicolon", "ls;", true},
		{"ampersand", "ls&", true},
		{"pipe", "ls|", true},
		{"greater than", "ls>", true},
		{"less than", "ls<", true},
		{"newline", "ls\n", true},
		{"carriage return", "ls\r", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := s.HasForbiddenCharsInCommand(tt.cmd); got != tt.want {
				t.Errorf("HasForbiddenCharsInCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

func TestSafetyService_IsSafeGitSubcommand(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name string
		sub  string
		want bool
	}{
		{"status", "status", true},
		{"log", "log", true},
		{"push", "push", false},
		{"commit", "commit", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := s.IsSafeGitSubcommand(tt.sub); got != tt.want {
				t.Errorf("IsSafeGitSubcommand(%q) = %v, want %v", tt.sub, got, tt.want)
			}
		})
	}
}

func TestSafetyService_IsSafeGoSubcommand(t *testing.T) {
	t.Parallel()
	s := NewSafetyService(DefaultPolicy())
	tests := []struct {
		name string
		sub  string
		want bool
	}{
		{"list", "list", true},
		{"test", "test", true},
		{"build", "build", false},
		{"run", "run", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := s.IsSafeGoSubcommand(tt.sub); got != tt.want {
				t.Errorf("IsSafeGoSubcommand(%q) = %v, want %v", tt.sub, got, tt.want)
			}
		})
	}
}
