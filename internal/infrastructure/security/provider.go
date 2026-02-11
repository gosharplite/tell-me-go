// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"

	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// SecurityProvider defines the interface for path validation and destructive action confirmation.
type SecurityProvider interface {
	IsPathSafe(path string) (string, error)
	IsPathWritable(path string) (string, error)
	ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error)
	TerminalLock()
	TerminalUnlock()
	IsCommandAllowed(command string) bool
	LogAudit(label1, val1, label2, val2 string)
	Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
	GetPolicy() *domain.Policy
	GetSafetyService() *domain.SafetyService
}
