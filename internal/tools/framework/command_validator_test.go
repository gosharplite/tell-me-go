// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestCommandValidator(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterReadOnlyPath("/etc")
	v := NewCommandValidator(sm)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"ls -la", true},
		{"rm -rf /", false},
		{"ls; rm -rf /", false},
		{"ls | grep foo", false},
		{"cat /etc/passwd", true},
		{"cat /etc/shadow", true},
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
	v := NewCommandValidator(nil)

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
		{"ls;echo", true},                  // Attached operator in first token
		{"ls>out", true},                   // Attached operator in first token
		{"grep \"foo && bar\" file.go", false}, // Contains operator but not standalone and NOT in first token
		{"sh -c \"ls && echo hi\"", false},      // Operator is inside another string
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
	sm := security.NewSecurityManager(nil)
	v := NewCommandValidator(sm)

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
	sm := security.NewSecurityManager(nil)
	v := NewCommandValidator(sm)

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
	sm := security.NewSecurityManager(nil)
	sm.RegisterSafePath("/safe")
	v := NewCommandValidator(sm)

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
	v := NewCommandValidator(nil)
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
	sm := security.NewSecurityManager(nil)
	v := NewCommandValidator(sm)
	allowed, reason := v.IsSafe("")
	if allowed {
		t.Error("expected IsSafe to return false for empty command")
	}
	if reason != "empty command" {
		t.Errorf("unexpected reason: %s", reason)
	}
}

func TestCommandValidator_UnsafeChars(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	v := NewCommandValidator(sm)
	
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
	sm := security.NewSecurityManager(nil)
	v := NewCommandValidator(sm)

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
