// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package toolstest provides test doubles for interfaces consumed by the
// internal/tools layer (command executors, security managers, command
// validators, user interactors).
//
// Helpers in this package satisfy domain/tools, domain/security, and
// related domain interfaces. They are intended only for use from
// _test.go files. Production code must never import this package.
package toolstest

import (
	"context"
)

// MockExecutor is a test double for tools.CommandExecutor. It captures
// the most recent command name and argument list it received and
// returns the pre-set OutputBytes/Error pair from both Output and
// CombinedOutput. LookPath returns a synthetic /usr/bin/<file> path.
type MockExecutor struct {
	OutputBytes []byte
	Error       error
	CommandName string
	CommandArgs []string
	CallCount   int
}

// Output records the command and returns the pre-set output and error.
func (m *MockExecutor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CallCount++
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// CombinedOutput records the command and returns the pre-set output and error.
func (m *MockExecutor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	m.CallCount++
	m.CommandName = name
	m.CommandArgs = args
	return m.OutputBytes, m.Error
}

// Reset zeroes all call-tracking fields so the same MockExecutor can be
// reused across subtests.
func (m *MockExecutor) Reset() {
	m.CallCount = 0
	m.CommandName = ""
	m.CommandArgs = nil
	m.OutputBytes = nil
	m.Error = nil
}

// LookPath simulates looking for an executable in the path.
func (m *MockExecutor) LookPath(file string) (string, error) {
	return "/usr/bin/" + file, nil
}
