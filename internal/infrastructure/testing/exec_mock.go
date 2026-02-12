// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package inframock

import (
	"context"
)

// MockExecutor implements tools.CommandExecutor for testing.
type MockExecutor struct {
	OutputBytes []byte
	Error       error
	CommandName string
	CommandArgs []string
}

func (m *MockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

func (m *MockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}
