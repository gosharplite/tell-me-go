// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"strings"
	"sync"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// interactionHandler manages terminal locking and user prompts via a UserInteractor.
type interactionHandler struct {
	terminalMu         sync.Mutex
	auditor            auditLogger
	interactorProvider func() domain.UserInteractor
}

// newInteractionHandler creates a new interactionHandler.
func newInteractionHandler(interactorProvider func() domain.UserInteractor, auditor auditLogger) *interactionHandler {
	return &interactionHandler{
		auditor:            auditor,
		interactorProvider: interactorProvider,
	}
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

	ui := h.interactorProvider()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if bypass {
		return h.handleBypassConfirmation(ui, action, target, detailLog)
	}
	return h.handleConfirmationPrompt(ctx, ui, action, target, detail, detailLog)
}

// handleBypassConfirmation logs the auto-approved action and returns true.
func (h *interactionHandler) handleBypassConfirmation(ui domain.UserInteractor, action, target, detailLog string) (bool, error) {
	ui.Warn(fmt.Sprintf("[Auto-Approved] Action '%s' on '%s' auto-approved (bypass_confirmation enabled).", action, target))
	if h.auditor != nil {
		h.auditor.LogAudit("CONFIRM_ACTION",
			"ACTION", action+" on "+target,
			"DETAIL", detailLog+" (auto-approved via bypass_confirmation)",
		)
	}
	return true, nil
}

// handleConfirmationPrompt builds the confirmation message, prompts the user,
// and logs the audit trail on approval.
func (h *interactionHandler) handleConfirmationPrompt(ctx context.Context, ui domain.UserInteractor, action, target, detail, detailLog string) (bool, error) {
	var sb strings.Builder
	fmt.Fprintf(&sb, "[CONFIRMATION REQUIRED]\nAI is requesting to %s: %s\n", action, target)
	if detail != "" {
		displayDetail := detail
		if len(displayDetail) > 1000 {
			displayDetail = displayDetail[:1000] + "\n... (truncated)"
		}
		sb.WriteString(displayDetail + "\n")
	}
	sb.WriteString("Proceed? (y/N) ")

	confirmed, err := ui.Confirm(ctx, sb.String())
	if err != nil {
		return false, err
	}

	if confirmed {
		h.logActionAudit(action, target, detailLog)
		return true, nil
	}
	return false, nil
}

// logActionAudit records a CONFIRM_ACTION audit entry when an action is approved.
func (h *interactionHandler) logActionAudit(action, target, detailLog string) {
	if h.auditor != nil {
		h.auditor.LogAudit("CONFIRM_ACTION",
			"ACTION", action+" on "+target,
			"DETAIL", detailLog,
		)
	}
}

// ReadSingleKey waits for a single key press.
func (h *interactionHandler) ReadSingleKey(ctx context.Context) (string, error) {
	ui := h.interactorProvider()
	return ui.ReadSingleKey(ctx)
}

// ReadLine reads a line of input.
func (h *interactionHandler) ReadLine(ctx context.Context) (string, error) {
	ui := h.interactorProvider()
	return ui.ReadLine(ctx)
}

// noOpInteractor is a dummy interactor that does nothing and denies all confirmations.
type noOpInteractor struct{}

// defaultNoOp is the singleton no-op interactor returned by NoOpInteractor.
// noOpInteractor holds no state, so a single shared instance avoids per-call
// allocation on the hot path (every SecurityManager interaction calls the
// provider).
var defaultNoOp domain.UserInteractor = &noOpInteractor{}

// NoOpInteractor returns a UserInteractor that does nothing and denies all confirmations.
// The returned value is a process-wide singleton; callers must not assume identity
// across calls is meaningful, but may rely on it being non-nil.
func NoOpInteractor() domain.UserInteractor {
	return defaultNoOp
}

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
