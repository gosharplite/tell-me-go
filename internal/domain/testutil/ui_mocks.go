// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package testutil

import (
	"context"
	"io"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

// MockUIRenderer is a mock implementation of ports.UIRenderer.
type MockUIRenderer struct {
	mock.Mock
}

func (m *MockUIRenderer) StartSpinner(ctx context.Context) func() {
	args := m.Called(ctx)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *MockUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *MockUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	args := m.Called(ctx, status)
	if fn, ok := args.Get(0).(func()); ok {
		return fn
	}
	return func() {}
}

func (m *MockUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.Called(ctx, content, showThoughts, rawOutput)
}

func (m *MockUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.Called(ctx, status)
}

func (m *MockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.Called(ctx, metrics, logFile, startTime)
}

func (m *MockUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.Called(ctx, calls, turn, maxTurns, showTools)
}

func (m *MockUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.Called(ctx, name, result, showTools)
}

func (m *MockUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.Called(ctx, msg, level)
}

func (m *MockUIRenderer) SetUseColor(use bool) {
	m.Called(use)
}

func (m *MockUIRenderer) SetForceSpinner(force bool) {
	m.Called(force)
}

// MockHistoryRenderer is a mock implementation of ports.HistoryRenderer.
type MockHistoryRenderer struct {
	mock.Mock
}

func (m *MockHistoryRenderer) Render(w io.Writer, h ports.HistoryReader, n int, options ports.HistoryRenderOptions) {
	m.Called(w, h, n, options)
}
