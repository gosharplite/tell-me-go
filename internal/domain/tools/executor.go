// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
)

// CommandExecutor defines the interface for executing system commands.
// It mirrors os/exec.Cmd but accepts a context for cancellation.
type CommandExecutor interface {
	// Output runs the command and returns its standard output.
	// If the command exits with a non-zero status, the error is of type
	// *exec.ExitError and includes the stderr output.
	Output(ctx context.Context, name string, args ...string) ([]byte, error)

	// CombinedOutput runs the command and returns its combined standard
	// output and standard error.
	CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error)

	// LookPath searches for an executable named file in the directories
	// named by the PATH environment variable. It returns the absolute
	// path if found, or an error if not.
	LookPath(file string) (string, error)
}
