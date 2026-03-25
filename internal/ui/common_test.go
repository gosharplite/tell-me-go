// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

type mockClock struct {
	clock.Clock
	now time.Time
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Sleep(d time.Duration)                  {}
func (m *mockClock) After(d time.Duration) <-chan time.Time { return nil }
func (m *mockClock) NewTicker(d time.Duration) clock.Ticker {
	return mockTicker{c: nil}
}
func (m *mockClock) Jitter(base float64) float64 { return base }

type mockTicker struct {
	c <-chan time.Time
}

func (m mockTicker) C() <-chan time.Time { return m.c }
func (m mockTicker) Stop()               {}

type mockLocker struct {
	locked bool
	mu     sync.Mutex
}

func (m *mockLocker) TerminalLock() {
	m.mu.Lock()
	m.locked = true
}

func (m *mockLocker) TerminalUnlock() {
	m.locked = false
	m.mu.Unlock()
}

func (m *mockLocker) IsPathSafe(path string) (string, error)     { return path, nil }
func (m *mockLocker) IsPathWritable(path string) (string, error) { return path, nil }
func (m *mockLocker) IsBypassActive() bool                       { return false }
func (m *mockLocker) IsCommandAllowed(command string) bool       { return true }
func (m *mockLocker) Prompt(message string)                      {}
func (m *mockLocker) Warn(message string)                        {}
func (m *mockLocker) ReadLine(ctx context.Context) (string, error) {
	return "", nil
}
func (m *mockLocker) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *mockLocker) LogAudit(action string, args ...any) {}
func (m *mockLocker) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}

func (m *mockLocker) Close() error { return nil }
