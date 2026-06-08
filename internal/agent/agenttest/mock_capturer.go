// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// MockCapturer is a hand-rolled mutex-guarded spy implementing both
// ports.Capturer and security.UserInteractor. Configure behaviour
// by setting function fields; nil means zero-value return.
type MockCapturer struct {
	mu sync.Mutex

	// Function fields — nil means return zero value
	IsTTYFn         func(v any) bool
	CapturePromptFn func(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error)
	ConfirmFn       func(ctx context.Context, message string) (bool, error)
	CloseFn         func(ctx context.Context) error
	WarnFn          func(msg string)
	PromptFn        func(msg string)
	ReadSingleKeyFn func(ctx context.Context) (string, error)
	ReadLineFn      func(ctx context.Context) (string, error)

	// Spy counters
	isTTYCalls         int
	capturePromptCalls int
	confirmCalls       int
	closeCalls         int
	warnCalls          int
	promptCalls        int
	readSingleKeyCalls int
	readLineCalls      int
	calledMethods      []string
}

// Compile-time interface assertions
var (
	_ ports.Capturer          = (*MockCapturer)(nil)
	_ security.UserInteractor = (*MockCapturer)(nil)
)

func (m *MockCapturer) IsTTY(v any) bool {
	m.mu.Lock()
	m.isTTYCalls++
	m.calledMethods = append(m.calledMethods, "IsTTY")
	fn := m.IsTTYFn
	m.mu.Unlock()

	if fn != nil {
		return fn(v)
	}
	return false
}

func (m *MockCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	m.mu.Lock()
	m.capturePromptCalls++
	m.calledMethods = append(m.calledMethods, "CapturePrompt")
	fn := m.CapturePromptFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, args, opts...)
	}
	return "", nil
}

func (m *MockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	m.mu.Lock()
	m.confirmCalls++
	m.calledMethods = append(m.calledMethods, "Confirm")
	fn := m.ConfirmFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, message)
	}
	return false, nil
}

func (m *MockCapturer) Close(ctx context.Context) error {
	m.mu.Lock()
	m.closeCalls++
	m.calledMethods = append(m.calledMethods, "Close")
	fn := m.CloseFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return nil
}

func (m *MockCapturer) Warn(msg string) {
	m.mu.Lock()
	m.warnCalls++
	m.calledMethods = append(m.calledMethods, "Warn")
	fn := m.WarnFn
	m.mu.Unlock()

	if fn != nil {
		fn(msg)
	}
}

func (m *MockCapturer) Prompt(msg string) {
	m.mu.Lock()
	m.promptCalls++
	m.calledMethods = append(m.calledMethods, "Prompt")
	fn := m.PromptFn
	m.mu.Unlock()

	if fn != nil {
		fn(msg)
	}
}

func (m *MockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	m.mu.Lock()
	m.readSingleKeyCalls++
	m.calledMethods = append(m.calledMethods, "ReadSingleKey")
	fn := m.ReadSingleKeyFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return "", nil
}

func (m *MockCapturer) ReadLine(ctx context.Context) (string, error) {
	m.mu.Lock()
	m.readLineCalls++
	m.calledMethods = append(m.calledMethods, "ReadLine")
	fn := m.ReadLineFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return "", nil
}

// Snapshot returns the current spy counters and a copy of the called-methods
// list in a concurrency-safe manner.
func (m *MockCapturer) Snapshot() (
	isTTYCalls, capturePromptCalls, confirmCalls, closeCalls,
	warnCalls, promptCalls, readSingleKeyCalls, readLineCalls int,
	methods []string,
) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calledMethods))
	copy(out, m.calledMethods)
	return m.isTTYCalls, m.capturePromptCalls, m.confirmCalls, m.closeCalls,
		m.warnCalls, m.promptCalls, m.readSingleKeyCalls, m.readLineCalls, out
}
