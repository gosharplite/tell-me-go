// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
)

// CommandExecutor defines the interface for executing system commands.
type CommandExecutor interface {
	Output(ctx context.Context, name string, args ...string) ([]byte, error)
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)
}
