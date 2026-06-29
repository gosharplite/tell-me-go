// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"errors"
	"fmt"
	"os"
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
			wantTruncated: true, // Buffer filled to capacity → truncated flag set
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

func TestShellTool_AuthorizeAndAuditPipeline_OutputFileError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}

	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	// Block the output file path to force resolveOutputFile to fail
	sm.AllowAll = false
	sm.SetBypassActive(false)
	sm.IsWritableFunc = func(path string) (string, error) {
		return "", fmt.Errorf("permission denied on output path")
	}

	helperSlash := filepath.ToSlash(helperPath)
	res, err := tool.PipeCommands(ctx, map[string]interface{}{
		"commands":    []interface{}{fmt.Sprintf("%s echo hello", helperSlash), fmt.Sprintf("%s grep hello", helperSlash)},
		"reason":      "test output file error",
		"output_file": "/root/forbidden.txt",
	}, nil)

	// The error should be in res.Error
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected res.Error to be non-nil")
	}
	if !strings.Contains(res.Error.Error(), "permission denied") {
		t.Errorf("expected 'permission denied' in res.Error, got: %v", res.Error)
	}
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

func TestShellTool_ExecuteCommand_AuthorizeError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}

	mockSec := &mockShellSecurity{
		MockSecurityManager: sm,
		AuthorizeFunc: func(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
			return false, fmt.Errorf("authorization service down")
		},
	}
	tool := newTestShellTool(mockSec, validator)
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s echo hello", helperSlash),
		"reason":  "test auth error",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected res.Error from authorization failure")
	}
	if !strings.Contains(res.Error.Error(), "authorization service down") {
		t.Errorf("expected 'authorization service down' in res.Error, got: %v", res.Error)
	}
}

func TestShellTool_PrepareCommand_WrapShell(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)

	// Force HasShellFeatures to return true so prepareCommand calls wrapper.Wrap
	validator := &toolstest.MockCommandValidator{
		HasShellFeaturesFunc: func(parts []string) bool {
			return true
		},
	}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s echo hello", helperSlash),
		"reason":  "test shell wrap",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected res.Error: %v", res.Error)
	}
	if !strings.Contains(res.Text, "hello") {
		t.Errorf("expected output to contain 'hello', got: %s", res.Text)
	}
}

func TestShellTool_StartHeartbeat_WithChannel(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	hb := make(chan struct{}, 1)
	helperSlash := filepath.ToSlash(helperPath)

	// Pass a non-nil heartbeat channel to exercise the hb != nil path
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s echo quick", helperSlash),
		"reason":  "test heartbeat channel",
	}, hb)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected res.Error: %v", res.Error)
	}
	if !strings.Contains(res.Text, "quick") {
		t.Errorf("expected output to contain 'quick', got: %s", res.Text)
	}
}

func TestShellTool_StartHeartbeat_TickFires(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	validator := &toolstest.MockCommandValidator{}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	hb := make(chan struct{}, 1)
	helperSlash := filepath.ToSlash(helperPath)

	// Sleep long enough for the 2-second heartbeat ticker to fire at least once
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s sleep 3", helperSlash),
		"reason":  "test heartbeat tick fires",
		"timeout": 10,
	}, hb)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected res.Error: %v", res.Error)
	}
}

func TestShellTool_PrepareCommand_ValidateStructureAfterWrap(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)

	// shellPrefix returns the expected shell command prefix after wrapping.
	shellPrefix := "sh"
	if runtime.GOOS == "windows" {
		shellPrefix = "cmd.exe"
	}

	// HasShellFeatures returns true so Wrap is called, then ValidateStructure fails on wrapped parts
	validator := &toolstest.MockCommandValidator{
		HasShellFeaturesFunc: func(parts []string) bool {
			return true
		},
		ValidateStructureFunc: func(parts []string) error {
			// Fail specifically for shell-wrapped parts (shellPrefix, "-c", ...)
			if len(parts) > 0 && parts[0] == shellPrefix {
				return fmt.Errorf("shell wrapping validation failed")
			}
			return nil
		},
	}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	helperSlash := filepath.ToSlash(helperPath)
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": fmt.Sprintf("%s echo hello", helperSlash),
		"reason":  "test validate after wrap",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected res.Error from ValidateStructure after wrapping")
	}
	if !strings.Contains(res.Error.Error(), "shell wrapping validation failed") {
		t.Errorf("expected 'shell wrapping validation failed' in res.Error, got: %v", res.Error)
	}
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

func TestWindowsTranslator_Translate_CP_MV(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "cp simple",
			input:    []string{"cp", "src.txt", "dst.txt"},
			expected: []string{"cmd", "/c", "copy", "src.txt", "dst.txt"},
		},
		{
			name:     "mv simple",
			input:    []string{"mv", "old.txt", "new.txt"},
			expected: []string{"cmd", "/c", "move", "old.txt", "new.txt"},
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

func TestShellTool_PipeCommands_UnmarshalError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	res, err := tool.PipeCommands(ctx, map[string]interface{}{
		"commands": "not_an_array",
		"reason":   "test unmarshal",
	}, nil)

	if err != nil {
		t.Errorf("expected nil Go error (domain outcome), got: %v", err)
	}
	if res.Error == nil {
		t.Error("expected res.Error to be non-nil")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' prefix in Text, got: %q", res.Text)
	}
}

func TestShellTool_PipeCommands_TooFewCommands(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	validator := &toolstest.MockCommandValidator{}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	res, err := tool.PipeCommands(ctx, map[string]interface{}{
		"commands": []interface{}{"ls"},
		"reason":   "test single command",
	}, nil)

	if err != nil {
		t.Errorf("expected nil Go error, got: %v", err)
	}
	if res.Error == nil {
		t.Error("expected res.Error to be non-nil")
	}
	if !strings.Contains(res.Text, "at least two commands") {
		t.Errorf("expected 'at least two commands' in Text, got: %q", res.Text)
	}
}

func TestShellTool_SplitPipelineErrors(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)

	t.Run("Split error", func(t *testing.T) {
		validator := &toolstest.MockCommandValidator{
			SplitFunc: func(cmd string) ([]string, error) {
				return nil, fmt.Errorf("split failure")
			},
		}
		tool := newTestShellTool(sm, validator)

		res, err := tool.splitPipeline([]string{"valid-cmd", "also-valid"})
		if err == nil {
			t.Fatal("expected split error")
		}
		if !strings.Contains(err.Error(), "error parsing command at index 0") {
			t.Errorf("expected 'error parsing command at index 0', got: %v", err)
		}
		_ = res
	})

	t.Run("ValidateStructure error", func(t *testing.T) {
		validator := &toolstest.MockCommandValidator{
			ValidateStructureFunc: func(parts []string) error {
				return fmt.Errorf("invalid structure")
			},
		}
		tool := newTestShellTool(sm, validator)

		res, err := tool.splitPipeline([]string{"valid-cmd", "also-valid"})
		if err == nil {
			t.Fatal("expected validate structure error")
		}
		if !strings.Contains(err.Error(), "invalid command at index 0") {
			t.Errorf("expected 'invalid command at index 0', got: %v", err)
		}
		_ = res
	})
}

func TestShellWrapper_Wrap(t *testing.T) {
	posix := &posixShellWrapper{}
	windows := &windowsShellWrapper{}

	tests := []struct {
		name    string
		wrapper shellWrapper
		command string
		parts   []string
		want    []string
		checkFn func(*testing.T, []string) // custom assertion for non-deterministic cases
	}{
		{
			name:    "posix/any command",
			wrapper: posix,
			command: "echo hello",
			parts:   []string{"echo", "hello"},
			want:    []string{"sh", "-c", "echo hello"},
		},
		{
			name:    "windows/powershell verb-noun",
			wrapper: windows,
			command: "Get-ChildItem .",
			parts:   []string{"Get-ChildItem", "."},
			checkFn: assertPowerShellWrapping("Get-ChildItem ."),
		},
		{
			name:    "windows/powershell env-var indicator",
			wrapper: windows,
			command: "echo $env:PATH",
			parts:   []string{"echo", "$env:PATH"},
			checkFn: assertPowerShellWrapping("echo $env:PATH"),
		},
		{
			name:    "windows/powershell subexpression",
			wrapper: windows,
			command: "echo $(whoami)",
			parts:   []string{"echo", "$(whoami)"},
			checkFn: assertPowerShellWrapping("echo $(whoami)"),
		},
		{
			name:    "windows/powershell cmdlet substring",
			wrapper: windows,
			command: "ls | Select-String foo",
			parts:   []string{"ls", "|", "Select-String", "foo"},
			checkFn: assertPowerShellWrapping("ls | Select-String foo"),
		},
		{
			name:    "windows/powershell ps alias",
			wrapper: windows,
			command: "cat file.txt",
			parts:   []string{"cat", "file.txt"},
			checkFn: assertPowerShellWrapping("cat file.txt"),
		},
		{
			name:    "windows/plain command",
			wrapper: windows,
			command: "dir /b",
			parts:   []string{"dir", "/b"},
			want:    []string{"cmd.exe", "/c", "dir /b"},
		},
		{
			name:    "windows/empty parts",
			wrapper: windows,
			command: "",
			parts:   []string{},
			want:    []string{"cmd.exe", "/c", ""},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.wrapper.Wrap(tt.command, tt.parts)
			if tt.checkFn != nil {
				tt.checkFn(t, got)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Wrap() = %v, want %v", got, tt.want)
			}
		})
	}
}

// assertPowerShellWrapping returns a check function that validates PowerShell-style
// wrapping without hardcoding the shell binary (pwsh vs powershell is host-dependent).
func assertPowerShellWrapping(expectedCommand string) func(*testing.T, []string) {
	return func(t *testing.T, got []string) {
		t.Helper()
		if len(got) < 5 {
			t.Fatalf("expected at least 5 elements, got %d: %v", len(got), got)
		}

		// First element must be pwsh or powershell (host-dependent via exec.LookPath)
		shell := got[0]
		if shell != "pwsh" && shell != "powershell" {
			t.Errorf("expected shell to be 'pwsh' or 'powershell', got %q", shell)
		}

		// Flags must appear in order
		if got[1] != "-NoProfile" {
			t.Errorf("expected -NoProfile at position 1, got %q", got[1])
		}
		if got[2] != "-NonInteractive" {
			t.Errorf("expected -NonInteractive at position 2, got %q", got[2])
		}
		if got[3] != "-Command" {
			t.Errorf("expected -Command at position 3, got %q", got[3])
		}

		// The command argument must carry the UTF-8 encoding prefix
		const utf8Prefix = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; "
		last := got[4]
		if !strings.HasPrefix(last, utf8Prefix) {
			t.Errorf("expected last element to start with %q, got %q", utf8Prefix, last)
		}
		if !strings.HasSuffix(last, expectedCommand) {
			t.Errorf("expected last element to end with %q, got %q", expectedCommand, last)
		}
	}
}

// TestWindowsShellWrapper_Wrap_PwshFound covers the pwsh LookPath success branch.
// It places a fake pwsh executable on PATH so that exec.LookPath("pwsh") succeeds.
// Cannot use t.Parallel() because t.Setenv is incompatible with parallel tests.
func TestWindowsShellWrapper_Wrap_PwshFound(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("pwsh lookup uses platform-specific extensions; only meaningful on Windows")
	}

	tmpDir := t.TempDir()
	pwshPath := filepath.Join(tmpDir, "pwsh.exe") // .exe required for exec.LookPath on Windows
	if err := os.WriteFile(pwshPath, []byte("@echo off\r\nexit /b 0\r\n"), 0755); err != nil {
		t.Fatalf("failed to create fake pwsh: %v", err)
	}
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	w := &windowsShellWrapper{}
	got := w.Wrap("Get-ChildItem .", []string{"Get-ChildItem", "."})

	if len(got) == 0 {
		t.Fatal("expected non-empty result")
	}
	if got[0] != "pwsh" {
		t.Errorf("expected shell to be 'pwsh' when pwsh is on PATH, got %q", got[0])
	}
	if got[1] != "-NoProfile" {
		t.Errorf("expected -NoProfile at position 1, got %q", got[1])
	}
	if got[2] != "-NonInteractive" {
		t.Errorf("expected -NonInteractive at position 2, got %q", got[2])
	}
	if got[3] != "-Command" {
		t.Errorf("expected -Command at position 3, got %q", got[3])
	}
	const utf8Prefix = "[Console]::OutputEncoding = [System.Text.Encoding]::UTF8; "
	if !strings.HasPrefix(got[4], utf8Prefix) {
		t.Errorf("expected UTF-8 prefix, got %q", got[4])
	}
}

// TestShellTool_ExecuteCommand_UnmarshalError covers the error-handling path
// in ExecuteCommand when tools.UnmarshalArgs fails (shell.go:207-209).
// Passing "not_an_int" for the Timeout (int) field triggers a JSON unmarshal
// failure, which the function must surface as a non-nil ToolResult.Error
// with an "Error:"-prefixed Text and a nil Go error.
func TestShellTool_ExecuteCommand_UnmarshalError(t *testing.T) {
	t.Parallel()

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newTestShellTool(sm, &toolstest.MockCommandValidator{})
	ctx := context.Background()

	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"timeout": "not_an_int",
	}, nil)

	if err != nil {
		t.Errorf("expected nil Go error (domain outcome), got: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected res.Error to be non-nil")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' prefix in Text, got: %q", res.Text)
	}
}

// TestShellTool_PipeCommands_SplitPipelineError covers the splitPipeline
// error-handling path in PipeCommands (shell.go:306-308). When
// authorizeAndAuditPipeline passes but splitPipeline fails on a command,
// the function must return a non-nil ToolResult.Error with an "Error:"-
// prefixed Text and a nil Go error.
func TestShellTool_PipeCommands_SplitPipelineError(t *testing.T) {
	t.Parallel()

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)

	// SplitFunc fails for empty-string commands, triggering the
	// splitPipeline error branch inside PipeCommands.
	validator := &toolstest.MockCommandValidator{
		SplitFunc: func(cmd string) ([]string, error) {
			if cmd == "" {
				return nil, fmt.Errorf("split: empty command")
			}
			return strings.Fields(cmd), nil
		},
	}
	tool := newTestShellTool(sm, validator)
	ctx := context.Background()

	// Two entries pass len(commands) >= 2 in authorizeAndAuditPipeline,
	// but the second is empty and will fail Split in splitPipeline.
	res, err := tool.PipeCommands(ctx, map[string]interface{}{
		"commands": []interface{}{"echo hello", ""},
		"reason":   "testing split pipeline error",
	}, nil)

	if err != nil {
		t.Errorf("expected nil Go error (domain outcome), got: %v", err)
	}
	if res.Error == nil {
		t.Fatal("expected res.Error to be non-nil from splitPipeline failure")
	}
	if !strings.Contains(res.Text, "Error:") {
		t.Errorf("expected 'Error:' prefix in Text, got: %q", res.Text)
	}
	if !strings.Contains(res.Error.Error(), "split: empty command") {
		t.Errorf("expected 'split: empty command' in res.Error, got: %v", res.Error)
	}
}

// ---------------------------------------------------------------------------
// posixTranslator.Translate edge cases
// ---------------------------------------------------------------------------

func TestPosixTranslator_Translate_EdgeCases(t *testing.T) {
	p := &posixTranslator{}

	t.Run("nil slice", func(t *testing.T) {
		got := p.Translate(nil)
		if got != nil {
			t.Errorf("Translate(nil) = %v, want nil", got)
		}
	})

	t.Run("empty slice", func(t *testing.T) {
		got := p.Translate([]string{})
		if len(got) != 0 {
			t.Errorf("Translate([]) len = %d, want 0", len(got))
		}
	})

	t.Run("non-empty slice", func(t *testing.T) {
		input := []string{"echo", "hello"}
		got := p.Translate(input)
		if !reflect.DeepEqual(got, input) {
			t.Errorf("Translate(%v) = %v, want %v", input, got, input)
		}
	})
}

// ---------------------------------------------------------------------------
// windowsTranslator.Translate default passthrough & empty parts
// ---------------------------------------------------------------------------

func TestWindowsTranslator_Translate_DefaultPassthrough(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty parts",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "go command (passthrough)",
			input:    []string{"go", "test", "./..."},
			expected: []string{"go", "test", "./..."},
		},
		{
			name:     "git command (passthrough)",
			input:    []string{"git", "status"},
			expected: []string{"git", "status"},
		},
		{
			name:     "unknown command (passthrough)",
			input:    []string{"unknown-cmd", "--flag", "arg"},
			expected: []string{"unknown-cmd", "--flag", "arg"},
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

// ---------------------------------------------------------------------------
// windowsTranslator.Translate rm branches
// ---------------------------------------------------------------------------

func TestWindowsTranslator_Translate_RM(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "rm simple (non-recursive)",
			input:    []string{"rm", "file.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file.txt"},
		},
		{
			name:     "rm -r (recursive via -r)",
			input:    []string{"rm", "-r", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "rm -rf (recursive via -rf)",
			input:    []string{"rm", "-rf", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "rm -f (non-recursive, flag stripped)",
			input:    []string{"rm", "-f", "file.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file.txt"},
		},
		{
			name:     "rm -v (non-recursive, flag stripped)",
			input:    []string{"rm", "-v", "file.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "file.txt"},
		},
		{
			name:     "rm -rf -v dir (mixed flags, recursive)",
			input:    []string{"rm", "-rf", "-v", "dir"},
			expected: []string{"cmd", "/c", "rd", "/s", "/q", "dir"},
		},
		{
			name:     "rm multiple files (non-recursive)",
			input:    []string{"rm", "a.txt", "b.txt", "c.txt"},
			expected: []string{"cmd", "/c", "del", "/f", "/q", "a.txt", "b.txt", "c.txt"},
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

// ---------------------------------------------------------------------------
// windowsTranslator.Translate mkdir branches
// ---------------------------------------------------------------------------

func TestWindowsTranslator_Translate_Mkdir(t *testing.T) {
	w := &windowsTranslator{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "mkdir simple",
			input:    []string{"mkdir", "mydir"},
			expected: []string{"cmd", "/c", "mkdir", "mydir"},
		},
		{
			name:     "mkdir -p (flag stripped)",
			input:    []string{"mkdir", "-p", "a/b/c"},
			expected: []string{"cmd", "/c", "mkdir", "a/b/c"},
		},
		{
			name:     "mkdir -p multiple dirs",
			input:    []string{"mkdir", "-p", "a", "b", "c"},
			expected: []string{"cmd", "/c", "mkdir", "a", "b", "c"},
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

// ---------------------------------------------------------------------------
// shellTool.prepareCommand error paths
// ---------------------------------------------------------------------------

func TestShellTool_PrepareCommand_SplitError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	validator := &toolstest.MockCommandValidator{
		SplitFunc: func(cmd string) ([]string, error) {
			return nil, fmt.Errorf("split: mock failure")
		},
	}
	tool := newshellTool(sm, validator, &posixTranslator{}, &posixShellWrapper{})

	parts, err := tool.prepareCommand("any command")

	if err == nil {
		t.Fatal("expected error from prepareCommand, got nil")
	}
	if !strings.Contains(err.Error(), "error parsing command") {
		t.Errorf("expected 'error parsing command' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "split: mock failure") {
		t.Errorf("expected 'split: mock failure' in error, got: %v", err)
	}
	if parts != nil {
		t.Errorf("expected nil parts on error, got %v", parts)
	}
}

func TestShellTool_PrepareCommand_ValidateStructureError(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}

	validator := &toolstest.MockCommandValidator{
		// HasShellFeatures returns false so Wrap is NOT called;
		// ValidateStructure fails on the raw split parts.
		HasShellFeaturesFunc: func(parts []string) bool {
			return false
		},
		ValidateStructureFunc: func(parts []string) error {
			return fmt.Errorf("validate: structure rejected")
		},
	}
	tool := newshellTool(sm, validator, &posixTranslator{}, &posixShellWrapper{})

	parts, err := tool.prepareCommand("echo hello")

	if err == nil {
		t.Fatal("expected error from prepareCommand, got nil")
	}
	if !strings.Contains(err.Error(), "validate: structure rejected") {
		t.Errorf("expected 'validate: structure rejected' in error, got: %v", err)
	}
	if parts != nil {
		t.Errorf("expected nil parts on error, got %v", parts)
	}
}
