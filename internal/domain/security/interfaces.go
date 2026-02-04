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
