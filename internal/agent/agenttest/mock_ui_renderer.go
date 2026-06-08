// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// MockUIRenderer is a hand-rolled test double for ports.UIRenderer.
// Override function fields to script behaviour. All methods record
// invocation counts and names accessible via Snapshot(). The spinner
// methods return a no-op func() when their function field is nil.
//
// All methods are safe for concurrent use. Function fields are captured
// under the mutex and invoked outside it to avoid deadlocks.
type MockUIRenderer struct {
	mu                            sync.Mutex
	calledStartSpinner            int
	calledStartSpinnerWithStatus  int
	calledStartSpinnerWithMetrics int
	calledRenderResponse          int
	calledLogTurnStatus           int
	calledLogUsage                int
	calledLogToolCall             int
	calledLogToolResult           int
	calledLogSystemMessage        int
	calledRenderHealthReport      int
	calledSetUseColor             int
	calledSetForceSpinner         int
	calledIsTerminalContext       int
	calledMethods                 []string

	// Function fields — set before test to script behaviour.
	StartSpinnerFn            func(ctx context.Context) func()
	StartSpinnerWithStatusFn  func(ctx context.Context, status string) func()
	StartSpinnerWithMetricsFn func(ctx context.Context, status string) func()
	RenderResponseFn          func(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool)
	LogTurnStatusFn           func(ctx context.Context, status events.TurnStatus)
	LogUsageFn                func(ctx context.Context, m *llm.Metrics, logFile string, startTime time.Time)
	LogToolCallFn             func(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool)
	LogToolResultFn           func(ctx context.Context, name string, result tools.ToolResult, showTools bool)
	LogSystemMessageFn        func(ctx context.Context, msg string, level string)
	RenderHealthReportFn      func(ctx context.Context, report *ports.HealthReport)
	SetUseColorFn             func(use bool)
	SetForceSpinnerFn         func(force bool)
	IsTerminalContextFn       func() bool
}

// UIRendererSnapshot holds a race-safe copy of mock call counts and method names.
type UIRendererSnapshot struct {
	StartSpinner, StartSpinnerWithStatus, StartSpinnerWithMetrics int
	RenderResponse, LogTurnStatus, LogUsage                       int
	LogToolCall, LogToolResult, LogSystemMessage                  int
	RenderHealthReport, SetUseColor, SetForceSpinner              int
	IsTerminalContext                                             int
	Methods                                                       []string
}

// Snapshot returns a race-safe copy of invocation counts and ordered method names.
func (m *MockUIRenderer) Snapshot() UIRendererSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	methods := make([]string, len(m.calledMethods))
	copy(methods, m.calledMethods)
	return UIRendererSnapshot{
		StartSpinner:            m.calledStartSpinner,
		StartSpinnerWithStatus:  m.calledStartSpinnerWithStatus,
		StartSpinnerWithMetrics: m.calledStartSpinnerWithMetrics,
		RenderResponse:          m.calledRenderResponse,
		LogTurnStatus:           m.calledLogTurnStatus,
		LogUsage:                m.calledLogUsage,
		LogToolCall:             m.calledLogToolCall,
		LogToolResult:           m.calledLogToolResult,
		LogSystemMessage:        m.calledLogSystemMessage,
		RenderHealthReport:      m.calledRenderHealthReport,
		SetUseColor:             m.calledSetUseColor,
		SetForceSpinner:         m.calledSetForceSpinner,
		IsTerminalContext:       m.calledIsTerminalContext,
		Methods:                 methods,
	}
}

// ---- ResponseRenderer methods ----

func (m *MockUIRenderer) StartSpinner(ctx context.Context) func() {
	m.mu.Lock()
	m.calledStartSpinner++
	m.calledMethods = append(m.calledMethods, "StartSpinner")
	fn := m.StartSpinnerFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx)
	}
	return func() {}
}

func (m *MockUIRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	m.mu.Lock()
	m.calledStartSpinnerWithStatus++
	m.calledMethods = append(m.calledMethods, "StartSpinnerWithStatus")
	fn := m.StartSpinnerWithStatusFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, status)
	}
	return func() {}
}

func (m *MockUIRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	m.mu.Lock()
	m.calledStartSpinnerWithMetrics++
	m.calledMethods = append(m.calledMethods, "StartSpinnerWithMetrics")
	fn := m.StartSpinnerWithMetricsFn
	m.mu.Unlock()

	if fn != nil {
		return fn(ctx, status)
	}
	return func() {}
}

func (m *MockUIRenderer) RenderResponse(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
	m.mu.Lock()
	m.calledRenderResponse++
	m.calledMethods = append(m.calledMethods, "RenderResponse")
	fn := m.RenderResponseFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, content, showThoughts, rawOutput)
	}
}

// ---- StatusLogger methods ----

func (m *MockUIRenderer) LogTurnStatus(ctx context.Context, status events.TurnStatus) {
	m.mu.Lock()
	m.calledLogTurnStatus++
	m.calledMethods = append(m.calledMethods, "LogTurnStatus")
	fn := m.LogTurnStatusFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, status)
	}
}

func (m *MockUIRenderer) LogSystemMessage(ctx context.Context, msg string, level string) {
	m.mu.Lock()
	m.calledLogSystemMessage++
	m.calledMethods = append(m.calledMethods, "LogSystemMessage")
	fn := m.LogSystemMessageFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, msg, level)
	}
}

func (m *MockUIRenderer) RenderHealthReport(ctx context.Context, report *ports.HealthReport) {
	m.mu.Lock()
	m.calledRenderHealthReport++
	m.calledMethods = append(m.calledMethods, "RenderHealthReport")
	fn := m.RenderHealthReportFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, report)
	}
}

// ---- UsageLogger methods ----

func (m *MockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.mu.Lock()
	m.calledLogUsage++
	m.calledMethods = append(m.calledMethods, "LogUsage")
	fn := m.LogUsageFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, metrics, logFile, startTime)
	}
}

// ---- ToolLogger methods ----

func (m *MockUIRenderer) LogToolCall(ctx context.Context, calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.mu.Lock()
	m.calledLogToolCall++
	m.calledMethods = append(m.calledMethods, "LogToolCall")
	fn := m.LogToolCallFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, calls, turn, maxTurns, showTools)
	}
}

func (m *MockUIRenderer) LogToolResult(ctx context.Context, name string, result tools.ToolResult, showTools bool) {
	m.mu.Lock()
	m.calledLogToolResult++
	m.calledMethods = append(m.calledMethods, "LogToolResult")
	fn := m.LogToolResultFn
	m.mu.Unlock()

	if fn != nil {
		fn(ctx, name, result, showTools)
	}
}

// ---- RendererConfigurator methods ----

// SwapLogSystemMessageFn atomically replaces LogSystemMessageFn and returns
// the previous value. Safe for concurrent use with method calls.
func (m *MockUIRenderer) SwapLogSystemMessageFn(fn func(ctx context.Context, msg string, level string)) func(ctx context.Context, msg string, level string) {
	m.mu.Lock()
	prev := m.LogSystemMessageFn
	m.LogSystemMessageFn = fn
	m.mu.Unlock()
	return prev
}

func (m *MockUIRenderer) SetUseColor(use bool) {
	m.mu.Lock()
	m.calledSetUseColor++
	m.calledMethods = append(m.calledMethods, "SetUseColor")
	fn := m.SetUseColorFn
	m.mu.Unlock()

	if fn != nil {
		fn(use)
	}
}

func (m *MockUIRenderer) SetForceSpinner(force bool) {
	m.mu.Lock()
	m.calledSetForceSpinner++
	m.calledMethods = append(m.calledMethods, "SetForceSpinner")
	fn := m.SetForceSpinnerFn
	m.mu.Unlock()

	if fn != nil {
		fn(force)
	}
}

func (m *MockUIRenderer) IsTerminalContext() bool {
	m.mu.Lock()
	m.calledIsTerminalContext++
	m.calledMethods = append(m.calledMethods, "IsTerminalContext")
	fn := m.IsTerminalContextFn
	m.mu.Unlock()

	if fn != nil {
		return fn()
	}
	return false
}
