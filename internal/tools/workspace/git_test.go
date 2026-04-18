// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

type mockGitExecutor struct {
	handler func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (m *mockGitExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.handler(ctx, name, args...)
}

func (m *mockGitExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return m.handler(ctx, name, args...)
}

func TestGitTools(t *testing.T) {
	// SecurityManager requires a reader for InteractionHandler

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	// Allow all paths for testing
	sm.SetBypassActive(true)

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
		mockOut  string
		mockErr  error
		expected string
		wantErr  bool
	}{
		{
			name:     "get_git_status",
			toolName: "get_git_status",
			args:     nil,
			mockOut:  "M  file.go\n?? untracked.go",
			expected: "M  file.go\n?? untracked.go",
		},
		{
			name:     "get_git_diff unstaged",
			toolName: "get_git_diff",
			args:     map[string]interface{}{"staged": false},
			mockOut:  "diff output",
			expected: "diff output",
		},
		{
			name:     "get_git_diff staged",
			toolName: "get_git_diff",
			args:     map[string]interface{}{"staged": true},
			mockOut:  "staged diff output",
			expected: "staged diff output",
		},
		{
			name:     "get_git_log default",
			toolName: "get_git_log",
			args:     nil,
			mockOut:  "commit1\ncommit2",
			expected: "commit1\ncommit2",
		},
		{
			name:     "get_git_log with limit",
			toolName: "get_git_log",
			args:     map[string]interface{}{"limit": 5},
			mockOut:  "commit1",
			expected: "commit1",
		},
		{
			name:     "get_git_show",
			toolName: "get_git_show",
			args:     map[string]interface{}{"hash": "abc"},
			mockOut:  "commit details",
			expected: "commit details",
		},
		{
			name:     "get_git_show truncated",
			toolName: "get_git_show",
			args:     map[string]interface{}{"hash": "abc"},
			mockOut:  strings.Repeat("line\n", 400),
			expected: strings.Join(strings.Split(strings.Repeat("line\n", 400), "\n")[:300], "\n") + "\n... (Output truncated) ...",
		},
		{
			name:     "get_git_blame",
			toolName: "get_git_blame",
			args:     map[string]interface{}{"filepath": "file.go"},
			mockOut:  "blame output",
			expected: "blame output",
		},
		{
			name:     "get_git_log invalid args",
			toolName: "get_git_log",
			args:     map[string]interface{}{"limit": "invalid"},
			wantErr:  true,
		},
		{
			// `hash` is declared Required for get_git_show. With the
			// central required-args guard in registry.Execute, this is
			// caught BEFORE the handler runs and returned as a
			// model-friendly ToolResult{Text: "Error: ..."} with nil
			// error — matching the prevailing convention used by
			// generateMermaidDiagram and others. See the doc-comment on
			// registry.Execute / validateRequiredArgs.
			name:     "get_git_show missing hash",
			toolName: "get_git_show",
			args:     map[string]interface{}{},
			expected: `Error: missing required parameter "hash" for tool "get_git_show"`,
		},
		{
			name:     "get_git_show invalid args",
			toolName: "get_git_show",
			args:     map[string]interface{}{"hash": 123},
			wantErr:  true,
		},
		{
			name:     "get_git_show command error",
			toolName: "get_git_show",
			args:     map[string]interface{}{"hash": "abc"},
			mockOut:  "error",
			mockErr:  fmt.Errorf("git fail"),
			expected: "error",
			wantErr:  true,
		},
		{
			name:     "get_git_diff invalid args",
			toolName: "get_git_diff",
			args:     map[string]interface{}{"staged": "invalid"},
			wantErr:  true,
		},
		{
			// `filepath` is declared Required for get_git_blame —
			// caught by the central guard. See "get_git_show missing
			// hash" above for the contract rationale.
			name:     "get_git_blame missing filepath",
			toolName: "get_git_blame",
			args:     map[string]interface{}{},
			expected: `Error: missing required parameter "filepath" for tool "get_git_blame"`,
		},
		{
			name:     "get_git_blame invalid args",
			toolName: "get_git_blame",
			args:     map[string]interface{}{"filepath": 123},
			wantErr:  true,
		},
		{
			name:     "git command failure",
			toolName: "get_git_status",
			mockOut:  "fatal: not a git repository",
			mockErr:  fmt.Errorf("exit status 128"),
			expected: "fatal: not a git repository",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &mockGitExecutor{
				handler: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					if name != "git" {
						return nil, fmt.Errorf("expected git, got %s", name)
					}
					return []byte(tt.mockOut), tt.mockErr
				},
			}

			reg := registry.New()
			if err := Register(reg, sm, executor, security.NewCommandValidator(sm, nil), persistencetest.NewPlainOSFileSystem(), nil); err != nil {
				t.Fatalf("Register failed: %v", err)
			}

			res, err := reg.Execute(context.Background(), tt.toolName, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && res.Text != tt.expected {
				t.Errorf("Execute() got = %q, want %q", res.Text, tt.expected)
			}
		})
	}
}

func TestGitDestructiveActions(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		args        map[string]interface{}
		approved    bool
		mockOut     string
		mockErr     error
		expected    string
		expectedErr string
		wantErr     bool
	}{
		{
			name:     "git_commit approved",
			toolName: "git_commit",
			args:     map[string]interface{}{"message": "feat: test", "reason": "ship it"},
			approved: true,
			mockOut:  "[main abc] feat: test",
			expected: "[main abc] feat: test",
		},
		{
			name:        "git_commit nothing to commit",
			toolName:    "git_commit",
			args:        map[string]interface{}{"message": "feat: test", "reason": "ship it"},
			approved:    true,
			mockOut:     "On branch main\nnothing to commit, working tree clean",
			mockErr:     fmt.Errorf("exit status 1"),
			expected:    "On branch main\nnothing to commit, working tree clean",
			expectedErr: "no staged changes. You must stage files first (e.g., using execute_command with 'git add .') before committing",
			wantErr:     true,
		},
		{
			name:     "git_create_branch approved",
			toolName: "git_create_branch",
			args:     map[string]interface{}{"name": "new-branch", "reason": "test"},
			approved: true,
			mockOut:  "Switched to a new branch 'new-branch'",
			expected: "Switched to a new branch 'new-branch'",
		},
		{
			// Both `message` AND `reason` are Required for git_commit.
			// The central guard catches the missing message (and reason)
			// and returns a ToolResult{Text: "Error: ..."} with nil err.
			name:     "git_commit missing message",
			toolName: "git_commit",
			args:     map[string]interface{}{},
			expected: `Error: missing required parameters [message reason] for tool "git_commit"`,
		},
		{
			// `message: 123` is type-invalid (not a string). The central
			// guard only checks PRESENCE, not type — so it passes,
			// 'reason' is also missing → central guard fires on reason.
			name:     "git_commit invalid args",
			toolName: "git_commit",
			args:     map[string]interface{}{"message": 123},
			expected: `Error: missing required parameter "reason" for tool "git_commit"`,
		},
		{
			// `name` is Required for git_create_branch — caught by the
			// central guard before the handler runs.
			name:     "git_create_branch missing name",
			toolName: "git_create_branch",
			args:     map[string]interface{}{"reason": "test"},
			expected: `Error: missing required parameter "name" for tool "git_create_branch"`,
		},
		{
			name:     "git_create_branch invalid args",
			toolName: "git_create_branch",
			args:     map[string]interface{}{"name": 123, "reason": "test"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sm := &toolstest.MockSecurityManager{AllowAll: true}

			executor := &mockGitExecutor{
				handler: func(ctx context.Context, name string, args ...string) ([]byte, error) {
					return []byte(tt.mockOut), tt.mockErr
				},
			}

			reg := registry.New()
			if err := Register(reg, sm, executor, security.NewCommandValidator(sm, nil), persistencetest.NewPlainOSFileSystem(), nil); err != nil {
				t.Fatalf("Register failed: %v", err)
			}

			res, err := reg.Execute(context.Background(), tt.toolName, tt.args, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.expectedErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.expectedErr) {
					t.Errorf("Execute() error = %v, want error containing %q", err, tt.expectedErr)
				}
			}
			if res.Text != tt.expected {
				t.Errorf("Execute() got = %v, want %v", res.Text, tt.expected)
			}
		})
	}
}

func TestGitBlameSafety(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: false}
	sm.IsSafeFunc = func(path string) (string, error) {
		if strings.Contains(path, "etc") {
			return "", fmt.Errorf("security violation")
		}
		return path, nil
	}
	// Do NOT set bypass.

	executor := &mockGitExecutor{
		handler: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return []byte("blame output"), nil
		},
	}

	reg := registry.New()
	if err := Register(reg, sm, executor, security.NewCommandValidator(sm, nil), persistencetest.NewPlainOSFileSystem(), nil); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Try to blame a file outside of allowed paths (assuming default policy denies it)
	_, err := reg.Execute(context.Background(), "get_git_blame", map[string]interface{}{"filepath": "/etc/passwd"}, nil)
	if err == nil {
		t.Error("Expected error for unauthorized path, got nil")
	}
}

func TestGitManagerInternal(t *testing.T) {
	// Test the runGitCommand failure branch explicitly if not covered

	m := &gitManager{
		Exec: &mockGitExecutor{
			handler: func(ctx context.Context, name string, args ...string) ([]byte, error) {
				return []byte("error detail"), fmt.Errorf("git fail")
			},
		},
	}
	out, err := m.runGitCommand(context.Background(), nil, "status")
	if err == nil {
		t.Error("expected error, got nil")
	}
	if out != "error detail" {
		t.Errorf("expected output 'error detail', got %q", out)
	}
}

func (m *mockGitExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
