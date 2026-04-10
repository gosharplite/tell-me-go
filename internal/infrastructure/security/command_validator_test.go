// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"strings"
	"testing"
)

func TestCommandValidator(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	sm.RegisterReadOnlyPath("/opt/test")
	v := NewCommandValidator(sm, nil)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"ls -la", true},
		{"rm -rf /", false},
		{"ls; rm -rf /", false},
		{"ls | grep foo", false},
		{"cat /opt/test/passwd", true},
		{"cat /opt/test/shadow", true},
		{"git status", true},
		{"git push", false},
	}

	for _, tt := range tests {
		allowed, _ := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v", tt.cmd, tt.allowed, allowed)
		}
	}
}

func TestCommandValidator_ValidateStructure(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator(nil, nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"ls -la", false},
		{"ls && echo hi", true},
		{"ls || echo hi", true},
		{"ls ; echo hi", true},
		{"ls | grep foo", true},
		{"ls > out.txt", true},
		{"ls >> out.txt", true},
		{"cat < in.txt", true},
		{"sleep 10 &", true},
		{"ls 2> err.txt", true},
		{"ls &> all.txt", true},
		{"ls |& grep foo", true},
		{"ls;echo", true},                      // Attached operator in first token
		{"ls>out", true},                       // Attached operator in first token
		{"grep \"foo && bar\" file.go", false}, // Contains operator but not standalone and NOT in first token
		{"sh -c \"ls && echo hi\"", false},     // Operator is inside another string
		{"go test ./$(id)", true},              // $ inside second token (interpolation)
		{"ls `id` /tmp", true},                 // ` inside token (interpolation)
		{"sh -c \"echo $HOME\"", false},        // Should be allowed in shell commands
		{"bash -c \"ls && echo hi\"", false},   // Should be allowed in shell commands
	}

	for _, tt := range tests {
		parts, err := v.Split(tt.cmd)
		if err != nil {
			t.Errorf("Split(%q) error: %v", tt.cmd, err)
			continue
		}
		err = v.ValidateStructure(parts)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateStructure(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
	}
}

func TestCommandValidator_Go(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	v := NewCommandValidator(sm, nil)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"go list ./...", true},
		{"go version", true},
		{"go help", true},
		{"go env", true},
		{"go vet", true},
		{"go run main.go", false},
		{"go build", false},
		{"go install", false},
		{"go test", true},
	}

	for _, tt := range tests {
		allowed, _ := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v", tt.cmd, tt.allowed, allowed)
		}
	}
}

func TestCommandValidator_Git(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	sm.RegisterReadOnlyPath("/tmp")
	v := NewCommandValidator(sm, nil)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"git status", true},
		{"git log", true},
		{"git diff", true},
		{"git branch", true},
		{"git show", true},
		{"git blame", true},
		{"git ls-files", true},
		{"git rev-parse HEAD", true},
		{"git tag", true},
		{"git remote -v", true},
		{"git describe", true},
		{"git commit", false},
		{"git push", false},
		{"git pull", false},
		{"git checkout", false},
		{"git -C /tmp status", true},
		{"git -c core.pager=cat status", true},
		{"git", false}, // Missing subcommand
	}

	for _, tt := range tests {
		allowed, _ := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v", tt.cmd, tt.allowed, allowed)
		}
	}
}

func TestCommandValidator_PathSafety(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	sm.RegisterSafePath("/safe")
	v := NewCommandValidator(sm, nil)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"ls /safe", true},
		{"ls /unsafe", false},
		{"ls .", true},
		{"ls ..", false}, // SecurityManager usually blocks ..
		{"go list ./...", true},
		{"grep foo --file=/safe/file", true},
		{"grep foo --file=/unsafe/file", false},
	}

	for _, tt := range tests {
		allowed, _ := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v", tt.cmd, tt.allowed, allowed)
		}
	}
}

func TestCommandValidator_SplitError(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator(nil, nil)
	_, err := v.Split("ls 'unclosed quote")
	if err == nil {
		t.Error("expected error for unclosed quote")
	}

	allowed, reason := v.IsSafe("ls 'unclosed quote")
	if allowed {
		t.Error("expected IsSafe to return false for invalid command")
	}
	if !strings.Contains(reason, "failed to parse command") {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestCommandValidator_EmptyCommand(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	v := NewCommandValidator(sm, nil)
	allowed, reason := v.IsSafe("")
	if allowed {
		t.Error("expected IsSafe to return false for empty command")
	}
	if reason != "empty command" {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestCommandValidator_UnsafeChars(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	v := NewCommandValidator(sm, nil)

	unsafe := []string{
		"ls\n",
		"ls\r",
		"ls $VAR",
		"ls `whoami`",
	}

	for _, cmd := range unsafe {
		allowed, _ := v.IsSafe(cmd)
		if allowed {
			t.Errorf("IsSafe(%q) should be false due to unsafe chars", cmd)
		}
	}
}

func TestCommandValidator_GranularAuthorization(t *testing.T) {
	t.Parallel()
	sm := NewSecurityManager(nil)
	v := NewCommandValidator(sm, nil)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		// New allowed commands
		{"golangci-lint run", true},
		{"staticcheck ./...", true},
		{"govulncheck ./...", true},

		// Expanded 'go' commands
		{"go test ./...", true},
		{"go test -v ./...", true},
		{"go test -bench=. -run=^$", true},
		{"go tool cover -func=coverage.out", true},

		// Forbidden 'go test' flags
		{"go test -o bin ./...", false},
		{"go test --output bin ./...", false},

		// Forbidden 'go tool' subcommands
		{"go tool pprof", false},
		{"go tool compile", false},

		// Existing 'go' commands
		{"go list ./...", true},
		{"go build", false},
	}

	for _, tt := range tests {
		allowed, reason := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v (reason: %s)", tt.cmd, tt.allowed, allowed, reason)
		}
	}
}

func TestTruncateOutput(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		max      int
		expected string
	}{
		{"a\nb\nc", 2, "a\nb\n... (Output truncated) ..."},
		{"a\nb\nc", 3, "a\nb\nc"},
		{"a\nb\nc", 5, "a\nb\nc"},
		{"", 5, ""},
		{"a", 0, "\n... (Output truncated) ..."},
	}

	for _, tt := range tests {
		got := truncateOutput(tt.input, tt.max)
		if got != tt.expected {
			t.Errorf("truncateOutput(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.expected)
		}
	}
}

func TestCommandValidator_WindowsShell(t *testing.T) {
	v := NewCommandValidator(nil, nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		// Unquoted: shell operators are standalone tokens
		{"cmd.exe /c ls && echo hi", false},
		{"cmd /c ls && echo hi", false},
		{"powershell -Command ls; echo hi", false},
		{"pwsh -c ls; echo hi", false},

		// This should always PASS
		{"sh -c ls && echo hi", false},
	}

	for _, tt := range tests {
		parts, err := v.Split(tt.cmd)
		if err != nil {
			t.Errorf("Split(%q) error: %v", tt.cmd, err)
			continue
		}
		err = v.ValidateStructure(parts)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateStructure(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
	}
}

func TestCommandValidator_HasShellFeatures(t *testing.T) {
	v := NewCommandValidator(nil, nil)

	tests := []struct {
		cmd  string
		want bool
	}{
		{"", false},
		{"ls", false},
		{"ls -la", false},
		{"ls && echo hi", true},
		{"ls | grep foo", true},
		{"ls > out.txt", true},
		{"echo $HOME", true},
		{"ls *.go", true},
		{"ls;echo hi", true},
		{"sh -c \"ls && echo hi\"", false},
		{"cmd.exe /c \"ls && echo hi\"", false},
		{"powershell -Command \"ls; echo hi\"", false},
	}

	for _, tt := range tests {
		parts, _ := v.Split(tt.cmd)
		got := v.HasShellFeatures(parts)
		if got != tt.want {
			t.Errorf("HasShellFeatures(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestCommandValidator_PathCorruption(t *testing.T) {
	t.Parallel()
	v := NewCommandValidator(nil, nil)

	tests := []struct {
		name    string
		cmd     string
		wantErr bool
	}{
		{"Windows path in quotes", `ls "C:\Users"`, true},
		{"Windows UNC path", `ls \\server\share`, true},
		{"Windows relative path with backslash", `ls .\internal\security`, true},
		{"Legitimate space escape", `ls file\ name`, false},
		{"Legitimate quote escape", `echo \"hello\"`, false},
		{"No backslashes", `ls /usr/bin`, false},
		{"Double backslash (escaped)", `ls C:\\Users`, true}, // We still want to force forward slashes
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := v.Split(tt.cmd)
			if (err != nil) != tt.wantErr {
				t.Errorf("Split(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !strings.Contains(err.Error(), "possible path corruption detected") {
				t.Errorf("Split(%q) error %v, want message containing 'possible path corruption detected'", tt.cmd, err)
			}
		})
	}
}
