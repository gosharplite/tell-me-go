// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// interactionHandler manages terminal locking and user prompts via a UserInteractor.
type interactionHandler struct {
	terminalMu sync.Mutex
	auditor    auditLogger
	interactor domain.UserInteractor
}

// newInteractionHandler creates a new interactionHandler.
func newInteractionHandler(interactor domain.UserInteractor, auditor auditLogger) *interactionHandler {
	return &interactionHandler{
		auditor:    auditor,
		interactor: interactor,
	}
}

// SetInteractor updates the user interactor.
func (h *interactionHandler) SetInteractor(interactor domain.UserInteractor) {
	h.interactor = interactor
}

// TerminalLock locks the terminal for exclusive access.
func (h *interactionHandler) TerminalLock() {
	h.terminalMu.Lock()
}

// TerminalUnlock unlocks the terminal.
func (h *interactionHandler) TerminalUnlock() {
	h.terminalMu.Unlock()
}

// ConfirmAction prompts the user for confirmation.
func (h *interactionHandler) ConfirmAction(ctx context.Context, action, target, detail string, bypass bool) (bool, error) {
	h.TerminalLock()
	defer h.TerminalUnlock()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if bypass {
		h.interactor.Warn(fmt.Sprintf("[Auto-Approved] Action '%s' on '%s' auto-approved (bypass_confirmation enabled).", action, target))
		if h.auditor != nil {
			h.auditor.LogAudit("ACTION", action+" on "+target, "DETAIL", detailLog+" (auto-approved via bypass_confirmation)")
		}
		return true, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[CONFIRMATION REQUIRED]\nAI is requesting to %s: %s\n", action, target))
	if detail != "" {
		if len(detail) > 1000 {
			detail = detail[:1000] + "\n... (truncated)"
		}
		sb.WriteString(detail + "\n")
	}
	sb.WriteString("Proceed? (y/N) ")

	confirmed, err := h.interactor.Confirm(ctx, sb.String())
	if err != nil {
		return false, err
	}

	if confirmed {
		if h.auditor != nil {
			h.auditor.LogAudit("ACTION", action+" on "+target, "DETAIL", detailLog)
		}
		return true, nil
	}
	return false, nil
}

// ReadSingleKey waits for a single key press.
func (h *interactionHandler) ReadSingleKey(ctx context.Context) (string, error) {
	return h.interactor.ReadSingleKey(ctx)
}

// ReadLine reads a line of input.
func (h *interactionHandler) ReadLine(ctx context.Context) (string, error) {
	return h.interactor.ReadLine(ctx)
}

// MockInteractor is a test helper that implements UserInteractor.
type MockInteractor struct {
	Answer string
	Err    error
	Warns  []string
}

// Confirm returns the mocked answer.
func (m *MockInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	default:
	}
	if m.Err != nil {
		return false, m.Err
	}
	ans := strings.ToLower(strings.TrimSpace(m.Answer))
	return ans == "y" || ans == "yes", nil
}

// Warn captures the warning message.
func (m *MockInteractor) Warn(message string) {
	m.Warns = append(m.Warns, message)
}

// Prompt captures the prompt message as a warning.
func (m *MockInteractor) Prompt(message string) {
	m.Warns = append(m.Warns, message)
}

// ReadSingleKey returns the first character of the mocked answer.
func (m *MockInteractor) ReadSingleKey(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", nil
	}
	return strings.ToLower(m.Answer[:1]), nil
}

// ReadLine returns the mocked answer.
func (m *MockInteractor) ReadLine(ctx context.Context) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	if m.Err != nil {
		return "", m.Err
	}
	if m.Answer == "" {
		return "", io.EOF
	}
	return m.Answer, nil
}

// noOpInteractor is a dummy interactor that does nothing and denies all confirmations.
type noOpInteractor struct{}

// Confirm always returns false.
func (i *noOpInteractor) Confirm(ctx context.Context, message string) (bool, error) {
	return false, nil
}

// Warn does nothing.
func (i *noOpInteractor) Warn(message string) {}

// Prompt does nothing.
func (i *noOpInteractor) Prompt(message string) {}

// ReadSingleKey returns an empty string.
func (i *noOpInteractor) ReadSingleKey(ctx context.Context) (string, error) {
	return "", nil
}

// ReadLine returns an empty string.
func (i *noOpInteractor) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}
