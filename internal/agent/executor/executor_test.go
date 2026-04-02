// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockToolRegistry struct {
	executeFn         func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	getDeclarationsFn func() []*tools.ToolDeclaration
	isSerial          bool
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if m.getDeclarationsFn != nil {
		return m.getDeclarationsFn()
	}
	return []*tools.ToolDeclaration{{Name: "test_tool"}}
}
func (m *mockToolRegistry) Register(d *tools.ToolDeclaration, f tools.ToolFunc) error { return nil }
func (m *mockToolRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.Register(def, handler)
}
func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args, hb)
	}
	return tools.ToolResult{Text: "ok"}, nil
}
func (m *mockToolRegistry) IsSerial(name string) bool      { return m.isSerial }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }
func (m *mockToolRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *mockToolRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *mockToolRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *mockToolRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockToolRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

func TestOrchestrator_ContextCancellation(t *testing.T) {
	t.Parallel()
	// Setup a tool that blocks until context is cancelled

	toolStarted := make(chan struct{})
	toolFinished := make(chan struct{})

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			close(toolStarted)
			timer := time.NewTimer(ciSafeTimeout)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				close(toolFinished)
				return tools.ToolResult{}, ctx.Err()
			case <-timer.C:
				return tools.ToolResult{Text: "timeout"}, nil
			}
		},
	}

	exec, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	// Run Execute in a goroutine
	var execErr error
	done := make(chan struct{})
	go func() {
		_, execErr = exec.Execute(ctx, respContent, 0, 10)
		close(done)
	}()

	// Wait for tool to start
	<-toolStarted

	// Cancel context
	cancel()

	// Wait for Execute to return
	timer1 := time.NewTimer(ciSafeTimeout)
	defer timer1.Stop()

	select {
	case <-done:
		// Success
	case <-timer1.C:
		t.Fatal("Execute did not return after context cancellation")
	}

	assert.Error(t, execErr)
	assert.Equal(t, context.Canceled, execErr)

	// Verify tool also finished
	timer2 := time.NewTimer(ciSafeTimeout)
	defer timer2.Stop()

	select {
	case <-toolFinished:
		// Success
	case <-timer2.C:
		t.Error("Tool implementation did not receive context cancellation")
	}
}

func TestWorkerPool_LeakPrevention(t *testing.T) {
	t.Parallel()
	pool := concurrency.NewWorkerPool(1)
	t.Cleanup(pool.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())

	started := make(chan struct{})
	finished := make(chan struct{})

	task := func(taskCtx context.Context) {
		close(started)
		select {
		case <-taskCtx.Done(): // Pool context
		case <-ctx.Done(): // Task context (our local one)
		}
		close(finished)
	}

	err := pool.Submit(task)
	require.NoError(t, err)
	<-started

	cancel() // Cancel the task context

	timer3 := time.NewTimer(100 * time.Millisecond)
	defer timer3.Stop()

	select {
	case <-finished:
		// Worker should be free now
	case <-timer3.C:
		t.Error("Worker did not release after task context cancellation")
	}

	// Pool should still be functional for other tasks
	task2Started := make(chan struct{})
	err = pool.Submit(func(ctx context.Context) {
		close(task2Started)
	})
	assert.NoError(t, err)

	timer4 := time.NewTimer(100 * time.Millisecond)
	defer timer4.Stop()

	select {
	case <-task2Started:
		// Success
	case <-timer4.C:
		t.Error("Worker pool became unresponsive after task cancellation")
	}
}

type orderMockRegistry struct {
	serialTools map[string]bool
}

func (m *orderMockRegistry) GetDeclarations() []*tools.ToolDeclaration                 { return nil }
func (m *orderMockRegistry) Register(d *tools.ToolDeclaration, f tools.ToolFunc) error { return nil }
func (m *orderMockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}
func (m *orderMockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (m *orderMockRegistry) IsSerial(name string) bool      { return m.serialTools[name] }
func (m *orderMockRegistry) IsLongRunning(name string) bool { return false }

func (m *orderMockRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{Serial: m.IsSerial(name), LongRunning: m.IsLongRunning(name)}
}

func (m *orderMockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *orderMockRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *orderMockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *orderMockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *orderMockRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}

func TestOrchestrator_PoolClosed_FailsGracefully(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	exec, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	exec.Shutdown() // Deterministically close the pool

	calls := []*llm.FunctionCall{
		{ID: "1", Name: "test_tool"},
	}
	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: calls[0]},
		},
	}

	// Submitting parallel tools must deterministically hit !pool.Submit(task)
	// Execute returns nil error if tool results are collected even if they represent failures.
	resultsContent, execErr := exec.Execute(context.Background(), respContent, 0, 10)

	assert.NoError(t, execErr)
	assert.NotNil(t, resultsContent)
	assert.Len(t, resultsContent.Parts, 1)

	// Verify it contains the expected failure message
	resultStr := resultsContent.Parts[0].FunctionResponse.Response["result"].(string)
	assert.Contains(t, resultStr, "pool closed or context cancelled")
}

func TestOrchestrator_WithActiveTrace_RecordsExecution(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "tool success"}, nil
		},
	}
	exec, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	// Setup trace context
	trace := telemetry.NewTurnTrace()
	ctx := telemetry.ContextWithTrace(context.Background(), trace)

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	// Execute
	_, execErr := exec.Execute(ctx, respContent, 0, 10)
	assert.NoError(t, execErr)

	// Verify telemetry
	assert.Len(t, trace.ToolExecutions, 1)
	assert.Equal(t, "test_tool", trace.ToolExecutions[0].ToolName)
	assert.Equal(t, "success", trace.ToolExecutions[0].Status)
}

func TestNewOrchestrator_NilObserver(t *testing.T) {
	reg := &mockToolRegistry{}
	_, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, nil)
	require.Error(t, err)
	assert.Equal(t, "ExecutionObserver is required", err.Error())
}

func TestNewOrchestrator_NilRegistry(t *testing.T) {
	// Call with nil registry
	executor, err := BuildOrchestrator(nil, nil, nil, &ports.NoOpLogger{}, &MockLogger{})

	// Should return an error and a nil executor
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry is required")
	require.Nil(t, executor)
}

func TestNewOrchestrator_NilLogger(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	observer := &MockLogger{}

	// Explicitly pass nil for the logger
	_, err := BuildOrchestrator(reg, nil, nil, nil, observer)
	require.Error(t, err)
	assert.Equal(t, "logger is required", err.Error())
}

type capturingLogger struct {
	ports.NoOpLogger
	errorCalled bool
	lastMsg     string
	lastArgs    []any
}

func (l *capturingLogger) Error(msg string, args ...any) {
	l.errorCalled = true
	l.lastMsg = msg
	l.lastArgs = args
}

type errorEventBus struct {
	err error
}

func (m *errorEventBus) Publish(ctx context.Context, e events.Event) error { return m.err }
func (m *errorEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (m *errorEventBus) Shutdown(ctx context.Context) error                { return nil }
func (m *errorEventBus) Flush(ctx context.Context) error                   { return nil }

func TestOrchestrator_EmitEvent_ErrorLogging(t *testing.T) {
	// Setup
	mockLogger := &capturingLogger{}
	genericErr := errors.New("generic publish error")
	mockBus := &errorEventBus{err: genericErr}

	exec := &Orchestrator{
		logger: mockLogger,
	}

	ctx := context.Background()
	evt := events.SystemMessageEvent{Message: "test"}

	// Action: Call emitEvent which should log the error
	exec.emitEvent(ctx, mockBus, evt)

	// Assert
	assert.True(t, mockLogger.errorCalled, "Expected Error to be called on logger")
	assert.Equal(t, "event_publish_failed", mockLogger.lastMsg)

	// Check attributes
	attrs := make(map[string]any)
	for i := 0; i < len(mockLogger.lastArgs); i += 2 {
		if i+1 < len(mockLogger.lastArgs) {
			key, ok := mockLogger.lastArgs[i].(string)
			if ok {
				attrs[key] = mockLogger.lastArgs[i+1]
			}
		}
	}

	assert.Equal(t, "SystemMessageEvent", attrs["event_type"])
	assert.ErrorContains(t, attrs["error"].(error), "generic publish error")
}

func TestOrchestrator_EmitEvent_ErrBusNotInitialized_NoLogging(t *testing.T) {
	// Setup
	mockLogger := &capturingLogger{}
	mockBus := &errorEventBus{err: events.ErrBusNotInitialized}

	exec := &Orchestrator{
		logger: mockLogger,
	}

	ctx := context.Background()
	evt := events.SystemMessageEvent{Message: "test"}

	// Action: Call emitEvent which should NOT log ErrBusNotInitialized
	exec.emitEvent(ctx, mockBus, evt)

	// Assert
	assert.False(t, mockLogger.errorCalled, "Expected Error NOT to be called on logger for ErrBusNotInitialized")
}

type mockAuthorizer struct {
	RequestBatchConsentFunc func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool)
}

func (m *mockAuthorizer) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	return nil
}

func (m *mockAuthorizer) IdentifyConsentItems(calls []*llm.FunctionCall) ([]int, map[int]bool) {
	return nil, nil
}

func (m *mockAuthorizer) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	if m.RequestBatchConsentFunc != nil {
		return m.RequestBatchConsentFunc(ctx, calls)
	}
	return ctx, nil
}

func TestOrchestrator_ConsentEvents_DetachedContext(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	bus := &mockEventBus{}

	// Block RequestBatchConsent until we cancel the context
	consentStarted := make(chan struct{})
	canFinishConsent := make(chan struct{})

	auth := &mockAuthorizer{
		RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
			close(consentStarted)
			select {
			case <-canFinishConsent:
				return ctx, nil
			case <-ctx.Done():
				return ctx, nil
			}
		},
	}

	exec, err := BuildOrchestrator(reg, nil, bus, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	exec.pipeline.(*defaultToolPipeline).authorizer = auth
	t.Cleanup(exec.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	// Run Execute in a goroutine
	done := make(chan struct{})
	go func() {
		_, _ = exec.Execute(ctx, respContent, 0, 10)
		close(done)
	}()

	// Wait for consent to start
	<-consentStarted

	// Cancel the context - this should stop auth.RequestBatchConsent
	cancel()

	// Wait for Execute to return
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("Execute did not return after context cancellation")
	}

	// Verify events
	bus.mu.Lock()
	defer bus.mu.Unlock()

	var hasFinished bool
	for _, e := range bus.Published {
		if e.Type() == "ConsentFinishedEvent" {
			hasFinished = true
		}
	}

	assert.True(t, hasFinished, "ConsentFinishedEvent should be published even if context is cancelled")
}
