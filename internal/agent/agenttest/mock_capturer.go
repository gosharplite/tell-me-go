// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// MockCapturer is a testify-based mock of ports.Capturer covering the
// full prompt/confirmation/I-O surface area. Configure behaviour with
// mock.On(...) per method.
type MockCapturer struct {
	mock.Mock
}

func (m *MockCapturer) IsTTY(v any) bool {
	args := m.Called(v)
	return args.Bool(0)
}

func (m *MockCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	callArgs := m.Called(ctx, args, opts)
	return callArgs.String(0), callArgs.Error(1)
}

func (m *MockCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	args := m.Called(ctx, message)
	return args.Bool(0), args.Error(1)
}

func (m *MockCapturer) Close(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockCapturer) Warn(msg string)   { m.Called(msg) }
func (m *MockCapturer) Prompt(msg string) { m.Called(msg) }
func (m *MockCapturer) ReadSingleKey(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
func (m *MockCapturer) ReadLine(ctx context.Context) (string, error) {
	args := m.Called(ctx)
	return args.String(0), args.Error(1)
}
