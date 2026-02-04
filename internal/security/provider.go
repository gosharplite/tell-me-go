// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "context"

// SecurityProvider defines the interface for path validation and destructive action confirmation.
type SecurityProvider interface {
	IsPathSafe(path string) (string, error)
	IsPathWritable(path string) (string, error)
	ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error)
	TerminalLock()
	TerminalUnlock()
	IsCommandAllowed(command string) bool
}
