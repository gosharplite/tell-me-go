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

func (m *mockAuditor) SetLogFile(path string)                        {}
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
		name           string
		action         string
		target         string
		detail         string
		bypass         bool
		interactorAns  string
		interactorErr  error
		expectResult   bool
		expectErr      bool
		expectWarn     string
		expectAudit    string
		expectTruncLog bool
	}{
		{
			name:          "Standard Confirmation - Approved",
			action:        "delete",
			target:        "file.txt",
			detail:        "User wants to delete the file.",
			bypass:        false,
			interactorAns: "y",
			expectResult:  true,
			expectAudit:   "delete on file.txt",
		},
		{
			name:          "Standard Confirmation - Denied",
			action:        "delete",
			target:        "file.txt",
			detail:        "User wants to delete the file.",
			bypass:        false,
			interactorAns: "n",
			expectResult:  false,
		},
		{
			name:          "Bypass Active",
			action:        "delete",
			target:        "file.txt",
			detail:        "User wants to delete the file.",
			bypass:        true,
			expectResult:  true,
			expectWarn:    "[Auto-Approved]",
			expectAudit:   "delete on file.txt",
		},
		{
			name:           "Audit Logging with Truncation",
			action:         "write",
			target:         "large_file.txt",
			detail:         strings.Repeat("A", 600),
			bypass:         false,
			interactorAns:  "y",
			expectResult:   true,
			expectAudit:    "write on large_file.txt",
			expectTruncLog: true,
		},
		{
			name:          "User Prompt Truncation",
			action:        "write",
			target:        "large_file.txt",
			detail:        strings.Repeat("B", 1200),
			bypass:        false,
			interactorAns: "y",
			expectResult:  true,
		},
		{
			name:          "Interactor Error",
			action:        "delete",
			target:        "file.txt",
			bypass:        false,
			interactorErr: fmt.Errorf("interactor error"),
			expectErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyInteractor{}
			spy.Answer = tt.interactorAns
			spy.Err = tt.interactorErr
			auditor := &mockAuditor{}
			handler := newInteractionHandler(spy, auditor)

			ctx := context.Background()
			result, err := handler.ConfirmAction(ctx, tt.action, tt.target, tt.detail, tt.bypass)

			if (err != nil) != tt.expectErr {
				t.Errorf("ConfirmAction() error = %v, expectErr %v", err, tt.expectErr)
				return
			}

			if result != tt.expectResult {
				t.Errorf("ConfirmAction() result = %v, expectResult %v", result, tt.expectResult)
			}

			if tt.bypass {
				if len(spy.ConfirmCalls) > 0 {
					t.Error("Confirm() should not be called when bypass is active")
				}
				foundWarn := false
				for _, w := range spy.Warns {
					if strings.Contains(w, tt.expectWarn) {
						foundWarn = true
						break
					}
				}
				if !foundWarn {
					t.Errorf("Expected warning containing %q, but not found in %v", tt.expectWarn, spy.Warns)
				}
			} else if tt.interactorErr == nil {
				if len(spy.ConfirmCalls) == 0 {
					t.Error("Confirm() should be called when bypass is inactive")
				}
				if len(tt.detail) > 1000 {
					if !strings.Contains(spy.ConfirmCalls[0], "... (truncated)") {
						t.Error("Detail in user prompt should be truncated")
					}
				}
			}

			if tt.expectAudit != "" && (tt.expectResult || tt.bypass) {
				foundAudit := false
				for _, log := range auditor.Logs {
					if strings.Contains(log.Val1, tt.expectAudit) {
						foundAudit = true
						if tt.bypass && !strings.Contains(log.Val2, "(auto-approved via bypass_confirmation)") {
							t.Error("Audit log missing bypass suffix")
						}
						if tt.expectTruncLog && !strings.Contains(log.Val2, "... (truncated)") {
							t.Error("Audit log detail should be truncated")
						}
						break
					}
				}
				if !foundAudit {
					t.Errorf("Expected audit log for %q, but not found", tt.expectAudit)
				}
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
