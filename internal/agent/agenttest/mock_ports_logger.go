// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// warnEntry records a single Warn invocation, capturing the message and
// associated key-value argument pairs.
type warnEntry struct {
	Msg  string
	Args []any
}

// debugEntry records a single Debug invocation, capturing the message and
// associated key-value argument pairs.
type debugEntry struct {
	Msg  string
	Args []any
}

// MockPortsLogger implements ports.Logger and captures Error, Warn, and Debug
// calls for assertion. It is goroutine-safe.
type MockPortsLogger struct {
	mu     sync.Mutex
	Errors []string     // Each entry is the "msg" argument from an Error() call
	Warns  []warnEntry  // Each entry captures the "msg" and args from a Warn() call
	Debugs []debugEntry // Each entry captures the "msg" and args from a Debug() call
}

// Compile-time interface check
var _ ports.Logger = (*MockPortsLogger)(nil)

func (m *MockPortsLogger) Error(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors = append(m.Errors, msg)
}

// WarnCalledWith returns true if any Warn invocation had a matching message.
// The args key-value pairs are not compared.
func (m *MockPortsLogger) WarnCalledWith(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, w := range m.Warns {
		if w.Msg == msg {
			return true
		}
	}
	return false
}

// DebugCalledWith returns true if any Debug invocation had a matching message.
// The args key-value pairs are not compared.
func (m *MockPortsLogger) DebugCalledWith(msg string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.Debugs {
		if d.Msg == msg {
			return true
		}
	}
	return false
}

func (m *MockPortsLogger) Warn(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Warns = append(m.Warns, warnEntry{Msg: msg, Args: args})
}
func (m *MockPortsLogger) Info(msg string, args ...any) {}
func (m *MockPortsLogger) Debug(msg string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Debugs = append(m.Debugs, debugEntry{Msg: msg, Args: args})
}
