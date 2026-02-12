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
	Logs []auditEntry
}

type auditEntry struct {
	Label1, Val1, Label2, Val2 string
}

func (m *mockAuditor) LogAudit(label1, val1, label2, val2 string) {
	m.Logs = append(m.Logs, auditEntry{label1, val1, label2, val2})
}

func (m *mockAuditor) SetLogFile(path string)                         {}
func (m *mockAuditor) SetInteractor(interactor domain.UserInteractor) {}

// spyInteractor captures calls to UserInteractor methods.
type spyInteractor struct {
	MockInteractor
	ConfirmCalls []string
}

func (s *spyInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	s.ConfirmCalls = append(s.ConfirmCalls, message)
	return s.MockInteractor.Confirm(ctx, message)
}

func TestInteractionHandler_ConfirmAction(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		target     string
		detail     string
		bypass     bool
		mockSetup  func(m *MockInteractor)
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
			mockSetup: func(m *MockInteractor) {
				m.Answer = "y"
			},
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.NotEmpty(t, spy.ConfirmCalls)
				assert.Len(t, auditor.Logs, 1)
				assert.Contains(t, auditor.Logs[0].Val1, "delete on file.txt")
			},
		},
		{
			name:   "Standard Confirmation - Denied",
			action: "delete",
			target: "file.txt",
			detail: "User wants to delete the file.",
			bypass: false,
			mockSetup: func(m *MockInteractor) {
				m.Answer = "n"
			},
			wantResult: false,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.NotEmpty(t, spy.ConfirmCalls)
				assert.Empty(t, auditor.Logs)
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
				assert.Len(t, auditor.Logs, 1)
				assert.Contains(t, auditor.Logs[0].Val1, "delete on file.txt")
				assert.Contains(t, auditor.Logs[0].Val2, "(auto-approved via bypass_confirmation)")
			},
		},
		{
			name:   "Audit Logging with Truncation",
			action: "write",
			target: "large_file.txt",
			detail: strings.Repeat("A", 600),
			bypass: false,
			mockSetup: func(m *MockInteractor) {
				m.Answer = "y"
			},
			wantResult: true,
			verify: func(t *testing.T, spy *spyInteractor, auditor *mockAuditor) {
				assert.Len(t, auditor.Logs, 1)
				assert.Contains(t, auditor.Logs[0].Val1, "write on large_file.txt")
				assert.Contains(t, auditor.Logs[0].Val2, "... (truncated)")
			},
		},
		{
			name:   "User Prompt Truncation",
			action: "write",
			target: "large_file.txt",
			detail: strings.Repeat("B", 1200),
			bypass: false,
			mockSetup: func(m *MockInteractor) {
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
			mockSetup: func(m *MockInteractor) {
				m.Err = fmt.Errorf("interactor error")
			},
			wantErr:   true,
			errSubstr: "interactor error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			spy := &spyInteractor{}
			if tt.mockSetup != nil {
				tt.mockSetup(&spy.MockInteractor)
			}
			auditor := &mockAuditor{}
			handler := newInteractionHandler(spy, auditor)

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
	spy := &spyInteractor{}
	spy.Answer = "hello"
	handler := newInteractionHandler(spy, nil)

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

func TestInteractionHandler_SetInteractor(t *testing.T) {
	spy1 := &spyInteractor{}
	handler := newInteractionHandler(spy1, nil)
	if handler.interactor != spy1 {
		t.Error("Initial interactor mismatch")
	}

	spy2 := &spyInteractor{}
	handler.SetInteractor(spy2)
	if handler.interactor != spy2 {
		t.Error("SetInteractor failed")
	}
}

func TestNoOpInteractor(t *testing.T) {
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
	m := &MockInteractor{}
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

	m = &MockInteractor{Answer: ""}
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
	handler := newInteractionHandler(&noOpInteractor{}, nil)
	// Just verify they don't panic and can be called
	handler.TerminalLock()
	handler.TerminalUnlock()
}

func TestMockInteractor_Errors(t *testing.T) {
	m := &MockInteractor{Err: fmt.Errorf("read error")}
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
