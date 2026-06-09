// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
)

// MockCapturer is a hand-rolled stub implementing both ports.Capturer
// and security.UserInteractor. Configure behaviour by setting the *Fn
// function fields; a nil func means the zero-value return.
type MockCapturer struct {
	IsTTYFn         func(v any) bool
	CapturePromptFn func(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error)
	ConfirmFn       func(ctx context.Context, message string) (bool, error)
	CloseFn         func(ctx context.Context) error
	WarnFn          func(msg string)
	PromptFn        func(msg string)
	ReadSingleKeyFn func(ctx context.Context) (string, error)
	ReadLineFn      func(ctx context.Context) (string, error)
}

// Compile-time interface assertions
var (
	_ ports.Capturer          = (*MockCapturer)(nil)
	_ security.UserInteractor = (*MockCapturer)(nil)
)

func (m *MockCapturer) IsTTY(v any) bool {
	if m.IsTTYFn != nil {
		return m.IsTTYFn(v)
	}
	return false
}

func (m *MockCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	if m.CapturePromptFn != nil {
		return m.CapturePromptFn(ctx, args, opts...)
	}
	return "", nil
}

func (m *MockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	if m.ConfirmFn != nil {
		return m.ConfirmFn(ctx, message)
	}
	return false, nil
}

func (m *MockCapturer) Close(ctx context.Context) error {
	if m.CloseFn != nil {
		return m.CloseFn(ctx)
	}
	return nil
}

func (m *MockCapturer) Warn(msg string) {
	if m.WarnFn != nil {
		m.WarnFn(msg)
	}
}

func (m *MockCapturer) Prompt(msg string) {
	if m.PromptFn != nil {
		m.PromptFn(msg)
	}
}

func (m *MockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	if m.ReadSingleKeyFn != nil {
		return m.ReadSingleKeyFn(ctx)
	}
	return "", nil
}

func (m *MockCapturer) ReadLine(ctx context.Context) (string, error) {
	if m.ReadLineFn != nil {
		return m.ReadLineFn(ctx)
	}
	return "", nil
}
