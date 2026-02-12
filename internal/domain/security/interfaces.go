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
}

// UserInteractor defines the interface for user interactions (confirmations, warnings).
type UserInteractor interface {
	Confirm(ctx context.Context, message string) (bool, error)
	Warn(message string)
	ReadSingleKey(ctx context.Context) (string, error)
	ReadLine(ctx context.Context) (string, error)
}

