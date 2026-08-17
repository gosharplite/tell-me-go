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
	now      time.Time
	tickerCh chan time.Time // optional: if non-nil, NewTicker returns a ticker backed by this channel
}

func (m *mockClock) Now() time.Time {
	return m.now
}

func (m *mockClock) Since(t time.Time) time.Duration {
	return m.now.Sub(t)
}

func (m *mockClock) Sleep(d time.Duration)                  {}
func (m *mockClock) After(d time.Duration) <-chan time.Time { return nil }
func (m *mockClock) NewTicker(d time.Duration) clock.Ticker {
	if m.tickerCh != nil {
		return &mockTicker{c: m.tickerCh}
	}
	return mockTicker{c: nil}
}

// tick sends the current mock time through the ticker channel, triggering any
// goroutine waiting on the ticker. No-op if tickerCh is nil.
func (m *mockClock) tick() {
	if m.tickerCh != nil {
		m.tickerCh <- m.now
	}
}
func (m *mockClock) Jitter(base float64) float64 { return base }

// newMockClockWithTicker creates a mock clock with a single-slot buffered
// ticker channel, allowing tests to manually advance the ticker with the
// tick() method.
func newMockClockWithTicker(now time.Time) *mockClock {
	return &mockClock{
		now:      now,
		tickerCh: make(chan time.Time, 1),
	}
}

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
func (m *mockLocker) IsToolAllowed(command string) bool          { return true }
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
