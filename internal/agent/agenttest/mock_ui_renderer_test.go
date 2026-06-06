// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/mock"
)

func TestMockUIRenderer_StartSpinner_NilStopFunc(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	m := new(MockUIRenderer)
	// Return nil → should return a no-op func() (not nil).
	m.On("StartSpinner", ctx).Return(nil)

	stop := m.StartSpinner(ctx)
	if stop == nil {
		t.Fatal("got nil stop func; want non-nil no-op")
	}
	// Must not panic.
	stop()
	m.AssertExpectations(t)
}

func TestMockUIRenderer_StartSpinner_CustomStopFunc(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	called := false
	customStop := func() { called = true }

	m := new(MockUIRenderer)
	m.On("StartSpinner", ctx).Return(customStop)

	stop := m.StartSpinner(ctx)
	stop()
	if !called {
		t.Error("custom stop func was not called")
	}
	m.AssertExpectations(t)
}

func TestMockUIRenderer_StartSpinnerWithStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	status := "loading"
	called := false
	customStop := func() { called = true }

	m := new(MockUIRenderer)
	m.On("StartSpinnerWithStatus", ctx, status).Return(customStop)

	stop := m.StartSpinnerWithStatus(ctx, status)
	stop()
	if !called {
		t.Error("custom stop func was not called")
	}
	m.AssertExpectations(t)
}

func TestMockUIRenderer_StartSpinnerWithMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	status := "processing"
	called := false
	customStop := func() { called = true }

	m := new(MockUIRenderer)
	m.On("StartSpinnerWithMetrics", ctx, status).Return(customStop)

	stop := m.StartSpinnerWithMetrics(ctx, status)
	stop()
	if !called {
		t.Error("custom stop func was not called")
	}
	m.AssertExpectations(t)
}

func TestMockUIRenderer_RenderResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	content := &llm.Content{Role: "assistant"}
	showThoughts := true
	rawOutput := false

	m := new(MockUIRenderer)
	m.On("RenderResponse", ctx, content, showThoughts, rawOutput).Return()

	m.RenderResponse(ctx, content, showThoughts, rawOutput)
	m.AssertCalled(t, "RenderResponse", ctx, content, showThoughts, rawOutput)
}

func TestMockUIRenderer_LogTurnStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	status := events.TurnStatus{CurrentTurns: 1}

	m := new(MockUIRenderer)
	m.On("LogTurnStatus", ctx, status).Return()

	m.LogTurnStatus(ctx, status)
	m.AssertCalled(t, "LogTurnStatus", ctx, status)
}

func TestMockUIRenderer_LogUsage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	metrics := &llm.Metrics{PromptTokens: 100}
	logFile := "/tmp/log"
	startTime := time.Now()

	m := new(MockUIRenderer)
	m.On("LogUsage", ctx, metrics, logFile, mock.Anything).Return()

	m.LogUsage(ctx, metrics, logFile, startTime)
	m.AssertCalled(t, "LogUsage", ctx, metrics, logFile, mock.Anything)
}

func TestMockUIRenderer_LogToolCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	calls := []*llm.FunctionCall{{Name: "test"}}
	turn := 1
	maxTurns := 10
	showTools := true

	m := new(MockUIRenderer)
	m.On("LogToolCall", ctx, calls, turn, maxTurns, showTools).Return()

	m.LogToolCall(ctx, calls, turn, maxTurns, showTools)
	m.AssertCalled(t, "LogToolCall", ctx, calls, turn, maxTurns, showTools)
}

func TestMockUIRenderer_LogToolResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	name := "test-tool"
	result := tools.ToolResult{Text: "done"}
	showTools := false

	m := new(MockUIRenderer)
	m.On("LogToolResult", ctx, name, result, showTools).Return()

	m.LogToolResult(ctx, name, result, showTools)
	m.AssertCalled(t, "LogToolResult", ctx, name, result, showTools)
}

func TestMockUIRenderer_LogSystemMessage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	msg := "system message"
	level := "info"

	m := new(MockUIRenderer)
	m.On("LogSystemMessage", ctx, msg, level).Return()

	m.LogSystemMessage(ctx, msg, level)
	m.AssertCalled(t, "LogSystemMessage", ctx, msg, level)
}

func TestMockUIRenderer_RenderHealthReport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	report := &ports.HealthReport{OverallStatus: ports.StatusHealthy}

	m := new(MockUIRenderer)
	m.On("RenderHealthReport", ctx, report).Return()

	m.RenderHealthReport(ctx, report)
	m.AssertCalled(t, "RenderHealthReport", ctx, report)
}

func TestMockUIRenderer_SetUseColor(t *testing.T) {
	t.Parallel()

	m := new(MockUIRenderer)
	m.On("SetUseColor", true).Return()

	m.SetUseColor(true)
	m.AssertCalled(t, "SetUseColor", true)
}

func TestMockUIRenderer_SetForceSpinner(t *testing.T) {
	t.Parallel()

	m := new(MockUIRenderer)
	m.On("SetForceSpinner", false).Return()

	m.SetForceSpinner(false)
	m.AssertCalled(t, "SetForceSpinner", false)
}

func TestMockUIRenderer_IsTerminalContext_True(t *testing.T) {
	t.Parallel()

	m := new(MockUIRenderer)
	m.On("IsTerminalContext").Return(true)

	got := m.IsTerminalContext()
	if !got {
		t.Error("got false; want true")
	}
	m.AssertExpectations(t)
}

func TestMockUIRenderer_IsTerminalContext_False(t *testing.T) {
	t.Parallel()

	m := new(MockUIRenderer)
	m.On("IsTerminalContext").Return(false)

	got := m.IsTerminalContext()
	if got {
		t.Error("got true; want false")
	}
	m.AssertExpectations(t)
}
