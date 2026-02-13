// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "context"

type ISecurityManager interface {
	ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error)
	IsPathSafe(path string) (string, error)
	IsPathWritable(path string) (string, error)
	TerminalLock()
	TerminalUnlock()
	IsBypassActive() bool
	IsCommandAllowed(command string) bool
	Prompt(message string)
	Warn(message string)
	Confirm(ctx context.Context, message string) (bool, error)
	ReadLine(ctx context.Context) (string, error)
	LogAudit(label1, val1, label2, val2 string)
	Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
}

// UserInteractor defines the interface for user interactions (confirmations, warnings).
type UserInteractor interface {
	Confirm(ctx context.Context, message string) (bool, error)
	Warn(message string)
	Prompt(message string)
	ReadSingleKey(ctx context.Context) (string, error)
	ReadLine(ctx context.Context) (string, error)
}

// ICommandValidator defines the interface for command validation.
type ICommandValidator interface {
	IsSafe(command string) (bool, string)
	Split(cmd string) ([]string, error)
	ValidateStructure(parts []string) error
	CheckPathSafety(parts []string) (bool, string)
}
