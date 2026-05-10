// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
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
			res, err := tool.ExecuteCommand(ctx, args, nil)
			if err != nil {
				t.Fatalf("ExecuteCommand failed: %v", err)
			}
			verifyTruncationResult(t, res, tt.expectedPart, tt.forbiddenPart, tt.wantTruncated, tt.exactMatch)
		})
	}
}

func newTestShellTool(sm shellSecurity, validator domain_security.CommandValidator) *shellTool {
	var translator commandTranslator = &posixTranslator{}
	var wrapper shellWrapper = &posixShellWrapper{}
	if runtime.GOOS == "windows" {
		translator = &windowsTranslator{}
		wrapper = &windowsShellWrapper{}
	}
	return newshellTool(sm, validator, translator, wrapper)
}

func setupTruncationTest(t *testing.T) (*shellTool, context.Context, map[string]interface{}) {
	t.Helper()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)

	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})
	ctx := context.Background()
	// Use forward slashes for the helper path to avoid POSIX parser errors on Windows
	cmd := fmt.Sprintf("%s printf 世界", filepath.ToSlash(helperPath))
	args := map[string]interface{}{
		"command": cmd,
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

func TestShellTool_ExecuteCommand_EdgeCases(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})
	ctx := context.Background()

	t.Run("Empty command", func(t *testing.T) {
		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": ""}, nil)
		if res.Error == nil && err == nil {
			t.Error("expected error for empty command")
		}
	})

	t.Run("Invalid shlex", func(t *testing.T) {
		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{"command": "ls 'unclosed"}, nil)
		if res.Error == nil && err == nil {
			t.Error("expected error for invalid shlex")
		}
	})

	t.Run("Invalid output path", func(t *testing.T) {
		// Set up security error for the invalid path
		sm.AllowAll = false
		sm.SetBypassActive(false)
		sm.IsWritableFunc = func(path string) (string, error) {
			if strings.Contains(path, "secret.txt") {
				return "", errors.New("security violation")
			}
			return path, nil
		}
		res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
			"command":     "ls",
			"output_file": "/root/secret.txt",
		}, nil)
		if res.Error == nil && err == nil {
			t.Error("expected error for invalid output path")
		}
	})
}

func TestShellTool_ResolveOutputFile_Sanitation(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})

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
	ctx := context.Background()
	helperSlash := filepath.ToSlash(helperPath)

	tests := []struct {
		name         string
		commands     any
		setupMocks   func(sm *toolstest.MockSecurityManager, v *toolstest.MockCommandValidator)
		expectedText string
		wantErr      bool
	}{
		{
			name:         "Simple pipe",
			commands:     []interface{}{fmt.Sprintf("%s echo hello world", helperSlash), fmt.Sprintf("%s grep hello", helperSlash)},
			expectedText: "hello world",
		},
		{
			name:     "Pipe with invalid command",
			commands: []interface{}{fmt.Sprintf("%s echo hello", helperSlash), "invalid-cmd-12345"},
			wantErr:  true,
		},
		{
			name:     "Pipe with shell operators (denied)",
			commands: []interface{}{fmt.Sprintf("%s echo hello", helperSlash), "grep hi > out.txt"},
			setupMocks: func(sm *toolstest.MockSecurityManager, v *toolstest.MockCommandValidator) {
				sm.AllowAll = false
				sm.SetBypassActive(false)
				v.IsSafeFunc = func(cmd string) (bool, string) {
					if strings.Contains(cmd, ">") {
						return false, "redirection not allowed"
					}
					return true, ""
				}
			},
			wantErr: true,
		},
		{
			name:     "Empty commands list",
			commands: []interface{}{},
			wantErr:  true,
		},
		{
			name:     "Invalid commands type",
			commands: "not a list",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := setupPipeMocks(t, tt.setupMocks)

			args := map[string]interface{}{
				"commands": tt.commands,
				"reason":   "test pipe",
			}
			res, err := tool.PipeCommands(ctx, args, nil)

			if tt.wantErr {
				assertPipeError(t, err, res)
				return
			}
			if err != nil || res.Error != nil {
				t.Fatalf("unexpected error: %v, res.Error: %v", err, res.Error)
			}
			if tt.expectedText != "" && !strings.Contains(res.Text, tt.expectedText) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedText, res.Text)
			}
		})
	}
}

func setupPipeMocks(t *testing.T, setupMocks func(sm *toolstest.MockSecurityManager, v *toolstest.MockCommandValidator)) *shellTool {
	t.Helper()
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}
	if setupMocks != nil {
		setupMocks(sm, validator)
	}
	return newTestShellTool(sm, validator)
}

func assertPipeError(t *testing.T, err error, res tools.ToolResult) {
	t.Helper()
	if res.Error == nil && err == nil {
		t.Error("expected error, got none")
	}
}

func TestShellTool_SecurityVisibility(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true) // So we don't block on Authorize

	mockSM := &mockShellSecurity{MockSecurityManager: sm}
	validator := &toolstest.MockCommandValidator{}
	tool := newTestShellTool(mockSM, validator)
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)

	t.Run("ExecuteCommand with output file", func(t *testing.T) {
		args := map[string]interface{}{
			"command":     fmt.Sprintf("%s echo hello", helperSlash),
			"reason":      "testing visibility",
			"output_file": "out.txt",
			"append":      true,
		}

		_, err := tool.ExecuteCommand(ctx, args, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}

		// Verify Authorize detail includes redirection
		if !strings.Contains(mockSM.LastDetail, " >") {
			t.Errorf("Authorize detail should contain redirection, got: %q", mockSM.LastDetail)
		}
		if !strings.Contains(mockSM.LastDetail, " >> ") {
			t.Errorf("Authorize detail should contain append redirection ' >> ', got: %q", mockSM.LastDetail)
		}
		if !strings.Contains(mockSM.LastDetail, "out.txt") {
			t.Errorf("Authorize detail should contain output file 'out.txt', got: %q", mockSM.LastDetail)
		}

		// Verify Audit Log includes OUTPUT_FILE and APPEND
		assertAuditField(t, mockSM.LastAuditArgs, "OUTPUT_FILE", "out.txt")
		assertAuditField(t, mockSM.LastAuditArgs, "APPEND", true)
	})

	t.Run("PipeCommands with output file", func(t *testing.T) {
		args := map[string]interface{}{
			"commands":    []interface{}{fmt.Sprintf("%s echo hello", helperSlash), fmt.Sprintf("%s grep hello", helperSlash)},
			"reason":      "testing pipe visibility",
			"output_file": "pipe_out.txt",
			"append":      false,
		}

		_, err := tool.PipeCommands(ctx, args, nil)
		if err != nil {
			t.Fatalf("PipeCommands failed: %v", err)
		}

		// Verify Authorize detail includes redirection
		if !strings.Contains(mockSM.LastDetail, " > ") {
			t.Errorf("Authorize detail should contain redirection ' > ', got: %q", mockSM.LastDetail)
		}
		if strings.Contains(mockSM.LastDetail, " >> ") {
			t.Errorf("Authorize detail should NOT contain append redirection ' >> ', got: %q", mockSM.LastDetail)
		}
		if !strings.Contains(mockSM.LastDetail, "pipe_out.txt") {
			t.Errorf("Authorize detail should contain output file 'pipe_out.txt', got: %q", mockSM.LastDetail)
		}

		// Verify Audit Log includes OUTPUT_FILE and APPEND
		assertAuditField(t, mockSM.LastAuditArgs, "OUTPUT_FILE", "pipe_out.txt")
		assertAuditField(t, mockSM.LastAuditArgs, "APPEND", false)
	})
}

func assertAuditField(t *testing.T, args []any, key string, want any) {
	t.Helper() // MUST be included so failures trace back to the test file's caller line

	var got any
	var found bool
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) && args[i] == key {
			got = args[i+1]
			found = true
			break
		}
	}

	if !found {
		t.Errorf("audit field %q not found in audit log", key)
		return
	}

	if got != want {
		t.Errorf("audit field %q: got %v; want %v", key, got, want)
	}
}

type mockShellSecurity struct {
	*toolstest.MockSecurityManager
	LastDetail      string
	LastAuditAction string
	LastAuditArgs   []any
	AuthorizeFunc   func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
}

func (m *mockShellSecurity) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	m.LastDetail = detail
	if m.AuthorizeFunc != nil {
		return m.AuthorizeFunc(ctx, label, detail, reason, isSafe)
	}
	return m.MockSecurityManager.Authorize(ctx, label, detail, reason, isSafe)
}

func (m *mockShellSecurity) LogAudit(action string, args ...any) {
	m.LastAuditAction = action
	m.LastAuditArgs = args
	m.MockSecurityManager.LogAudit(action, args...)
}

func TestShellTool_Authorization_Denials(t *testing.T) {
	t.Run("Authorization Denials", func(t *testing.T) {
		tests := []struct {
			name        string
			authResult  bool
			authErr     error
			expectedErr error
		}{
			{
				name:        "User explicitly declines",
				authResult:  false,
				authErr:     nil, // Simulates user typing 'N' at the prompt
				expectedErr: tools.ErrUserDeclined,
			},
			{
				name:        "Authorization context timeout",
				authResult:  false,
				authErr:     context.DeadlineExceeded,
				expectedErr: context.DeadlineExceeded,
			},
		}

		for _, tt := range tests {
			tt := tt // capture range variable for parallel execution safely
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				sm := &toolstest.MockSecurityManager{AllowAll: true}
				// 1. Setup the mock to return the table's simulated auth result
				mockSec := &mockShellSecurity{
					MockSecurityManager: sm,
					AuthorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
						return tt.authResult, tt.authErr
					},
				}

				validator := &toolstest.MockCommandValidator{}
				// 2. Initialize the tool with the mock
				tool := newTestShellTool(mockSec, validator)

				// 3. Action: Attempt to execute a command
				res, err := tool.ExecuteCommand(context.Background(), map[string]interface{}{
					"command": "rm -rf /",
					"reason":  "testing denial",
				}, nil)

				// 4. Assertion: Verify the exact sentinel error is returned
				if !errors.Is(res.Error, tt.expectedErr) && !errors.Is(err, tt.expectedErr) {
					t.Errorf("ExecuteCommand: expected error %v, got %v", tt.expectedErr, err)
				}

				// 5. Action: Attempt to execute piped commands
				res, err = tool.PipeCommands(context.Background(), map[string]interface{}{
					"commands": []interface{}{"ls", "grep foo"},
					"reason":   "testing denial",
				}, nil)

				// 6. Assertion: Verify the exact sentinel error is returned
				if !errors.Is(res.Error, tt.expectedErr) && !errors.Is(err, tt.expectedErr) {
					t.Errorf("PipeCommands: expected error %v, got %v", tt.expectedErr, err)
				}
			})
		}
	})
}

func TestShellTool_TimeoutParameter(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)

	t.Run("ExecuteCommand with timeout", func(t *testing.T) {
		args := map[string]interface{}{
			"command": fmt.Sprintf("%s echo hello", helperSlash),
			"reason":  "testing timeout parameter",
			"timeout": 123,
		}
		_, err := tool.ExecuteCommand(ctx, args, nil)
		if err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
	})

	t.Run("PipeCommands with timeout", func(t *testing.T) {
		args := map[string]interface{}{
			"commands": []interface{}{fmt.Sprintf("%s echo hello", helperSlash), fmt.Sprintf("%s grep hello", helperSlash)},
			"reason":   "testing timeout parameter",
			"timeout":  456,
		}
		_, err := tool.PipeCommands(ctx, args, nil)
		if err != nil {
			t.Fatalf("PipeCommands failed: %v", err)
		}
	})
}

func TestShellTool_PrepareCommand_ShellSelection(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}
	wrapper := &windowsShellWrapper{}
	_ = newshellTool(sm, validator, &posixTranslator{}, wrapper)

	t.Run("PowerShell indicators", func(t *testing.T) {
		tests := []struct {
			cmd   string
			parts []string
			want  bool
		}{
			{"Get-ChildItem", []string{"Get-ChildItem"}, true},
			{"Set-Location", []string{"Set-Location"}, true},
			{"echo $env:PATH", []string{"echo", "$env:PATH"}, true},
			{"ls | Select-String foo", []string{"ls", "|", "Select-String", "foo"}, true},
			{"go test ./...", []string{"go", "test", "./..."}, false},
			{"dir", []string{"dir"}, false},
			{"git-lfs", []string{"git-lfs"}, true}, // Note: Verb-Noun heuristic might over-match, but it's safe for shell wrapping.
		}

		for _, tt := range tests {
			got := wrapper.isPowerShellIndicator(tt.cmd, tt.parts)
			if got != tt.want {
				t.Errorf("isPowerShellIndicator(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		}
	})

	t.Run("HasShellFeatures for Cmdlets", func(t *testing.T) {
		tests := []struct {
			cmd  string
			want bool
		}{
			{"Get-ChildItem", true},
			{"Set-Location", true},
			{"go test", false},
			{"ls -la", false},
		}

		for _, tt := range tests {
			parts, _ := validator.Split(tt.cmd)
			got := validator.HasShellFeatures(parts)
			if got != tt.want {
				t.Errorf("HasShellFeatures(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		}
	})
}

func TestWindowsTranslator_Translate_LS(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "ls -la",
			input:    []string{"ls", "-la"},
			expected: []string{"cmd", "/c", "dir", "/A"},
		},
		{
			name:     "ls -R",
			input:    []string{"ls", "-R"},
			expected: []string{"cmd", "/c", "dir", "/S"},
		},
		{
			name:     "ls -laR",
			input:    []string{"ls", "-laR"},
			expected: []string{"cmd", "/c", "dir", "/S", "/A"},
		},
		{
			name:     "ls -l /some/path",
			input:    []string{"ls", "-l", "/some/path"},
			expected: []string{"cmd", "/c", "dir", "/some/path"},
		},
		{
			name:     "ls --recursive --all",
			input:    []string{"ls", "--recursive", "--all"},
			expected: []string{"cmd", "/c", "dir", "/S", "/A"},
		},
		{
			name:     "ls mixed",
			input:    []string{"ls", "-lhR", "/tmp", "-a"},
			expected: []string{"cmd", "/c", "dir", "/S", "/A", "/tmp"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := w.Translate(tt.input)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("Translate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestShellTool_TimeoutEnforcement(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)

	t.Run("ExecuteCommand timeout enforcement", func(t *testing.T) {
		// Sleep for 2 seconds with a 1 second timeout
		args := map[string]interface{}{
			"command": fmt.Sprintf("%s sleep 2", helperSlash),
			"reason":  "testing timeout enforcement",
			"timeout": 1,
		}
		res, err := tool.ExecuteCommand(ctx, args, nil)

		// The error can be in the return value or the ToolResult
		isTimeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(res.Error, context.DeadlineExceeded)
		if !isTimeout {
			t.Errorf("Expected timeout error (context.DeadlineExceeded), got err=%v, res.Error=%v", err, res.Error)
		}
	})

	t.Run("PipeCommands timeout enforcement", func(t *testing.T) {
		// Sleep for 2 seconds with a 1 second timeout
		args := map[string]interface{}{
			"commands": []interface{}{fmt.Sprintf("%s sleep 2", helperSlash), fmt.Sprintf("%s echo done", helperSlash)},
			"reason":   "testing pipe timeout enforcement",
			"timeout":  1,
		}
		res, err := tool.PipeCommands(ctx, args, nil)

		isTimeout := errors.Is(err, context.DeadlineExceeded) || errors.Is(res.Error, context.DeadlineExceeded)
		if !isTimeout {
			t.Errorf("Expected timeout error (context.DeadlineExceeded), got err=%v, res.Error=%v", err, res.Error)
		}
	})
}
