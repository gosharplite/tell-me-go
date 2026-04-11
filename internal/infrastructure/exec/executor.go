// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package exec

import (
	"context"
	"os/exec"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/encoding"
)

// RealExecutor is a production implementation of CommandExecutor that runs actual processes.
type RealExecutor struct{}

func (e *RealExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return encoding.DecodeBytes(out), err
}

func (e *RealExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return encoding.DecodeBytes(out), err
}

func (e *RealExecutor) LookPath(file string) (string, error) {
	return exec.LookPath(file)
}
