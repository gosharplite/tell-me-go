// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestShellTool_UTF8SafeTruncation(t *testing.T) {
	tool, ctx, args := setupTruncationTest(t)

	tests := []struct {
		name          string
		maxOutput     int
		expectedPart  string
		forbiddenPart string
		wantTruncated bool
		exactMatch    string
	}{
		{
			name:          "no truncation",
			maxOutput:     10,
			expectedPart:  "世界",
			wantTruncated: false,
		},
		{
			name:          "exact boundary",
			maxOutput:     7, // 6 bytes for "世界" + 1 byte for newline added by executor
			expectedPart:  "世界",
			wantTruncated: false,
		},
		{
			name:          "truncate middle of char",
			maxOutput:     5,
			expectedPart:  "世",
			forbiddenPart: "界",
			wantTruncated: true,
			exactMatch:    "世",
		},
		{
			name:          "truncate to first char",
			maxOutput:     3,
			expectedPart:  "世",
			forbiddenPart: "界",
			wantTruncated: true,
		},
		{
			name:          "truncate before first char",
			maxOutput:     2,
			expectedPart:  "",
			forbiddenPart: "世",
			wantTruncated: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool.maxOutput = tt.maxOutput
			res, err := tool.ExecuteCommand(ctx, args)
			if err != nil {
				t.Fatalf("ExecuteCommand failed: %v", err)
			}
			verifyTruncationResult(t, res, tt.expectedPart, tt.forbiddenPart, tt.wantTruncated, tt.exactMatch)
		})
	}
}

func setupTruncationTest(t *testing.T) (*shellTool, context.Context, map[string]interface{}) {
	t.Helper()
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))
	ctx := context.Background()
	args := map[string]interface{}{
		"command": `sh -c 'printf "世界"'`,
		"reason":  "testing utf8 truncation",
	}
	return tool, ctx, args
}

func verifyTruncationResult(t *testing.T, res tools.ToolResult, expected, forbidden string, wantTruncated bool, exactMatch string) {
	t.Helper()
	if expected != "" && !strings.Contains(res.Text, expected) {
		t.Errorf("expected output to contain %q, got %q", expected, res.Text)
	}
	if forbidden != "" && strings.Contains(res.Text, forbidden) {
		t.Errorf("output contains %q, truncation failed", forbidden)
	}

	hasIndicator := strings.Contains(res.Text, "(truncated)")
	if wantTruncated != hasIndicator {
		t.Errorf("wantTruncated=%v, but hasIndicator=%v. Output: %q", wantTruncated, hasIndicator, res.Text)
	}

	if exactMatch != "" {
		prefix := "Exit Code: 0\nOutput:\n"
		if !strings.HasPrefix(res.Text, prefix) {
			t.Fatalf("unexpected output format: %q", res.Text)
		}
		actual := strings.TrimPrefix(res.Text, prefix)
		actual = strings.TrimSuffix(actual, "\n... (truncated)")
		if actual != exactMatch {
			t.Errorf("expected exact match %q, got %q", exactMatch, actual)
		}
	}
}

func TestShellTool_ExecuteCommand_Validation(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))
	ctx := context.Background()

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Safe command",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "Blocked operator &&",
			command: "ls && echo hi",
			wantErr: true,
		},
		{
			name:    "Blocked operator ||",
			command: "ls || echo hi",
			wantErr: true,
		},
		{
			name:    "Blocked operator ;",
			command: "ls ; echo hi",
			wantErr: true,
		},
		{
			name:    "Blocked operator |",
			command: "ls | grep foo",
			wantErr: true,
		},
		{
			name:    "Blocked operator >",
			command: "ls > out.txt",
			wantErr: true,
		},
		{
			name:    "Operator inside sh -c",
			command: `sh -c "ls && echo hi"`,
			wantErr: false,
		},
		{
			name:    "Operator inside grep pattern",
			command: `grep "foo && bar" file.go`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.ExecuteCommand(ctx, map[string]interface{}{
				"command": tt.command,
				"reason":  "testing validation",
			})
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}

func TestShellTool_ExecuteCommand_EdgeCases(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))
	ctx := context.Background()

	t.Run("Empty command", func(t *testing.T) {
		_, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": ""})
		if err == nil {
			t.Error("expected error for empty command")
		}
	})

	t.Run("Invalid shlex", func(t *testing.T) {
		_, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "ls 'unclosed"})
		if err == nil {
			t.Error("expected error for invalid shlex")
		}
	})

	t.Run("Invalid output path", func(t *testing.T) {
		_, err := tool.ExecuteCommand(ctx, map[string]interface{}{
			"command":     "ls",
			"output_file": "/root/secret.txt",
		})
		if err == nil {
			t.Error("expected error for invalid output path")
		}
	})
}

func TestShellTool_ResolveOutputFile_Sanitation(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))

	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "Normal path",
			input:    "out.txt",
			expected: "out.txt",
			wantErr:  false,
		},
		{
			name:     "With spaces",
			input:    "  out.txt  ",
			expected: "out.txt",
			wantErr:  false,
		},
		{
			name:     "With null bytes",
			input:    "out\x00.txt",
			expected: "out.txt",
			wantErr:  false,
		},
		{
			name:     "With both",
			input:    "  out\x00.txt  ",
			expected: "out.txt",
			wantErr:  false,
		},
		{
			name:     "Becomes empty after sanitation",
			input:    "  \x00  ",
			expected: "",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tool.resolveOutputFile(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveOutputFile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.expected == "" {
				if got != "" {
					t.Errorf("resolveOutputFile() got = %q, want empty", got)
				}
				return
			}
			if filepath.Base(got) != tt.expected {
				t.Errorf("resolveOutputFile() got = %q, base should be %q", got, tt.expected)
			}
		})
	}
}

func TestShellTool_PipeCommands(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil))
	ctx := context.Background()

	t.Run("Simple pipe", func(t *testing.T) {
		args := map[string]interface{}{
			"commands": []interface{}{"echo hello world", "grep hello"},
			"reason":   "test pipe",
		}
		res, err := tool.PipeCommands(ctx, args)
		if err != nil {
			t.Fatalf("PipeCommands failed: %v", err)
		}
		if !strings.Contains(res.Text, "hello world") {
			t.Errorf("expected output to contain 'hello world', got %q", res.Text)
		}
	})

	t.Run("Pipe with invalid command", func(t *testing.T) {
		args := map[string]interface{}{
			"commands": []interface{}{"echo hello", "invalid-cmd-12345"},
			"reason":   "test pipe failure",
		}
		_, err := tool.PipeCommands(ctx, args)
		if err == nil {
			t.Error("expected error for invalid command in pipe")
		}
	})

	t.Run("Pipe with shell operators (denied)", func(t *testing.T) {
		args := map[string]interface{}{
			"commands": []interface{}{"echo hello", "grep hi > out.txt"},
			"reason":   "test pipe security",
		}
		_, err := tool.PipeCommands(ctx, args)
		if err == nil {
			t.Error("expected error for shell operator in pipe")
		}
	})

	t.Run("Empty commands list", func(t *testing.T) {
		_, err := tool.PipeCommands(ctx, map[string]interface{}{"commands": []interface{}{}})
		if err == nil {
			t.Error("expected error for empty commands list")
		}
	})

	t.Run("Invalid commands type", func(t *testing.T) {
		_, err := tool.PipeCommands(ctx, map[string]interface{}{"commands": "not a list"})
		if err == nil {
			t.Error("expected error for invalid commands type")
		}
	})
}
