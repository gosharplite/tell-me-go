// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MockPortsLogger implements ports.Logger and captures Error calls for assertion.
// It is goroutine-safe.
type MockPortsLogger struct {
	mu     sync.Mutex
	Errors []string // Each entry is the "msg" argument from an Error() call
}

// Compile-time interface check
var _ ports.Logger = (*MockPortsLogger)(nil)

func (m *MockPortsLogger) Error(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors = append(m.Errors, msg)
}

func (m *MockPortsLogger) Warn(msg string, args ...any)  {}
func (m *MockPortsLogger) Info(msg string, args ...any)  {}
func (m *MockPortsLogger) Debug(msg string, args ...any) {}
