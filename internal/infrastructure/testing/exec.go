// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
)

// mockExecutor implements tools.CommandExecutor for testing.
type mockExecutor struct {
	OutputBytes []byte
	Error       error
	CommandName string
	CommandArgs []string
}

func (m *mockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

func (m *mockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

func (m *mockExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
