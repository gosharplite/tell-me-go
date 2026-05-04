// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/stretchr/testify/assert"
)

// mockAuditor implements auditLogger for testing.
type mockAuditor struct {
	Actions []string
	Args    [][]any
}

func (m *mockAuditor) LogAudit(action string, args ...any) {
	m.Actions = append(m.Actions, action)
	m.Args = append(m.Args, args)
}

func (m *mockAuditor) getArgValue(callIdx int, key string) any {
	if callIdx >= len(m.Args) {
		return nil
	}
	args := m.Args[callIdx]
	for i := 0; i < len(args); i += 2 {
		if i+1 < len(args) && args[i] == key {
			return args[i+1]
		}
	}
	return nil
}

func (m *mockAuditor) SetLogFile(path string) {}
func (m *mockAuditor) Close() error           { return nil }

// spyInteractor captures calls to UserInteractor methods.
type spyInteractor struct {
	mockInteractor
	ConfirmCalls []string
}

func (s *spyInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	s.ConfirmCalls = append(s.ConfirmCalls, message)
	return s.mockInteractor.Confirm(ctx, message)
}

func TestInteractionHandler_ConfirmAction(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		action     string
		target     string
		detail     string
		bypass     bool
		mockSetup  func(m *mockInteractor)
		wantResult bool
		wantErr    bool
		errSubstr  string
		verify     func(t *testing.T, spy *spyInteractor, auditor *mockAuditor)
	}{
		{
			name:   "Standard Confirmation - Approved",
			action: "delete",
			target: "file.txt",
			detail: "User wants to delete the file.",
			bypass: false,
			mockSetup: func(m *mockInteractor) {
				m.Answer = "y"
			},
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.NotEmpty(t, spy.ConfirmCalls)
				assert.Len(t, auditor.Actions, 1)
				assert.Equal(t, "CONFIRM_ACTION", auditor.Actions[0])
				assert.Contains(t, auditor.getArgValue(0, "ACTION").(string), "delete on file.txt")
			},
		},
		{
			name:   "Standard Confirmation - Denied",
			action: "delete",
			target: "file.txt",
			detail: "User wants to delete the file.",
			bypass: false,
			mockSetup: func(m *mockInteractor) {
				m.Answer = "n"
			},
			wantResult: false,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.NotEmpty(t, spy.ConfirmCalls)
				assert.Empty(t, auditor.Actions)
			},
		},
		{
			name:       "Bypass Active",
			action:     "delete",
			target:     "file.txt",
			detail:     "User wants to delete the file.",
			bypass:     true,
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.Empty(t, spy.ConfirmCalls)
				assert.NotEmpty(t, spy.Warns)
				assert.Contains(t, spy.Warns[0], "[Auto-Approved]")
				assert.Len(t, auditor.Actions, 1)
				assert.Equal(t, "CONFIRM_ACTION", auditor.Actions[0])
				assert.Contains(t, auditor.getArgValue(0, "ACTION").(string), "delete on file.txt")
				assert.Contains(t, auditor.getArgValue(0, "DETAIL").(string), "(auto-approved via bypass_confirmation)")
			},
		},
		{
			name:   "Audit Logging with Truncation",
			action: "write",
			target: "large_file.txt",
			detail: strings.Repeat("A", 600),
			bypass: false,
			mockSetup: func(m *mockInteractor) {
				m.Answer = "y"
			},
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.Len(t, auditor.Actions, 1)
				assert.Equal(t, "CONFIRM_ACTION", auditor.Actions[0])
				assert.Contains(t, auditor.getArgValue(0, "ACTION").(string), "write on large_file.txt")
				assert.Contains(t, auditor.getArgValue(0, "DETAIL").(string), "... (truncated)")
			},
		},
		{
			name:   "User Prompt Truncation",
			action: "write",
			target: "large_file.txt",
			detail: strings.Repeat("B", 1200),
			bypass: false,
			mockSetup: func(m *mockInteractor) {
				m.Answer = "y"
			},
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.NotEmpty(t, spy.ConfirmCalls)
				assert.Contains(t, spy.ConfirmCalls[0], "... (truncated)")
			},
		},
		{
			name:   "Interactor Error",
			action: "delete",
			target: "file.txt",
			bypass: false,
			mockSetup: func(m *mockInteractor) {
				m.Err = fmt.Errorf("interactor error")
			},
			wantErr:   true,
			errSubstr: "interactor error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			spy := &spyInteractor{}
			if tt.mockSetup != nil {
				tt.mockSetup(&spy.mockInteractor)
			}
			auditor := &mockAuditor{}
			handler := newInteractionHandler(func() domain.UserInteractor { return spy }, auditor)

			got, err := handler.ConfirmAction(ctx, tt.action, tt.target, tt.detail, tt.bypass)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errSubstr != "" {
					assert.Contains(t, err.Error(), tt.errSubstr)
				}
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.wantResult, got)
			}

			if tt.verify != nil {
				tt.verify(t, spy, auditor)
			}
		})
	}
}

func TestInteractionHandler_ReadMethods(t *testing.T) {
	t.Parallel()
	spy := &spyInteractor{}
	spy.Answer = "hello"
	handler := newInteractionHandler(func() domain.UserInteractor { return spy }, nil)

	ctx := context.Background()
	key, err := handler.ReadSingleKey(ctx)
	if err != nil {
		t.Errorf("ReadSingleKey error: %v", err)
	}
	if key != "h" {
		t.Errorf("ReadSingleKey returned %q, expect 'h'", key)
	}

	line, err := handler.ReadLine(ctx)
	if err != nil {
		t.Errorf("ReadLine error: %v", err)
	}
	if line != "hello" {
		t.Errorf("ReadLine returned %q, expect 'hello'", line)
	}
}

func TestNoOpInteractor(t *testing.T) {
	t.Parallel()
	ni := &noOpInteractor{}
	ctx := context.Background()

	conf, err := ni.Confirm(ctx, "test")
	if err != nil || conf != false {
		t.Errorf("noOpInteractor.Confirm failed: %v, %v", err, conf)
	}

	ni.Warn("test")

	key, err := ni.ReadSingleKey(ctx)
	if err != nil || key != "" {
		t.Errorf("noOpInteractor.ReadSingleKey failed: %v, %v", err, key)
	}

	line, err := ni.ReadLine(ctx)
	if err != nil || line != "" {
		t.Errorf("noOpInteractor.ReadLine failed: %v, %v", err, line)
	}
}

func TestMockInteractor_EdgeCases(t *testing.T) {
	t.Parallel()
	m := &mockInteractor{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := m.Confirm(ctx, "test")
	if err == nil {
		t.Error("Expected error on canceled context in Confirm")
	}

	_, err = m.ReadSingleKey(ctx)
	if err == nil {
		t.Error("Expected error on canceled context in ReadSingleKey")
	}

	_, err = m.ReadLine(ctx)
	if err == nil {
		t.Error("Expected error on canceled context in ReadLine")
	}

	m = &mockInteractor{Answer: ""}
	key, err := m.ReadSingleKey(context.Background())
	if err != nil || key != "" {
		t.Errorf("Expected empty key on empty answer in ReadSingleKey, got %v, %v", err, key)
	}

	_, err = m.ReadLine(context.Background())
	if err != io.EOF {
		t.Errorf("Expected EOF on empty answer in ReadLine, got %v", err)
	}
}

func TestInteractionHandler_TerminalLocking(t *testing.T) {
	t.Parallel()
	handler := newInteractionHandler(NoOpInteractor, nil)
	// Just verify they don't panic and can be called
	handler.TerminalLock()
	handler.TerminalUnlock()
}

func TestMockInteractor_Errors(t *testing.T) {
	t.Parallel()
	m := &mockInteractor{Err: fmt.Errorf("read error")}
	ctx := context.Background()

	_, err := m.ReadSingleKey(ctx)
	if err == nil {
		t.Error("Expected error in ReadSingleKey")
	}

	_, err = m.ReadLine(ctx)
	if err == nil {
		t.Error("Expected error in ReadLine")
	}
}
