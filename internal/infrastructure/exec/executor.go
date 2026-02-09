// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package exec

import (
	"context"
	"os/exec"
)

// RealExecutor is a production implementation of CommandExecutor that runs actual processes.
type RealExecutor struct{}

func (e *RealExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

func (e *RealExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
