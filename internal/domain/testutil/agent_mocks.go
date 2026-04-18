// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"context"
)

// MockExecutor implements tools.CommandExecutor for testing.
//
// Bucket: TOOLS — slated for relocation into a internal/tools/toolstest
// helper package in a future session. See docs/refactor/testutil-audit.md.
type MockExecutor struct {
	OutputBytes []byte
	Error       error
	CommandName string
	CommandArgs []string
}

// Output records the command and returns the pre-set output and error.
func (m *MockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// CombinedOutput records the command and returns the pre-set output and error.
func (m *MockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// LookPath simulates looking for an executable in the path.
func (m *MockExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
