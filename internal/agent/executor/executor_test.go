// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

func TestDispatcher_ContextCancellation(t *testing.T) {
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

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

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

	t.Logf("execErr: %v", execErr)
	assert.Error(t, execErr)
	assert.ErrorIs(t, execErr, context.Canceled)

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

func TestDispatcher_PoolClosed_FailsGracefully(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

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
	// We no longer error on pool closed, we just use a local pool that's created per turn
	assert.Contains(t, resultStr, "ok")
}

func TestDispatcher_WithActiveTrace_RecordsExecution(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "tool success"}, nil
		},
	}
	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

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

func Test_newDispatcher_NilObserver(t *testing.T) {
	cfg := dispatcherConfig{}
	pipeline := &defaultToolPipeline{}
	_, err := newDispatcher(cfg, pipeline, nil, &ports.NoOpLogger{}, nil)
	require.Error(t, err)
	assert.Equal(t, "execution observer is required", err.Error())
}

func Test_newDispatcher_NilRegistry(t *testing.T) {
	cfg := dispatcherConfig{}
	executor, err := newDispatcher(cfg, nil, nil, &ports.NoOpLogger{}, &mockLogger{})

	// Should return an error and a nil executor
	require.Error(t, err)
	require.Contains(t, err.Error(), "pipeline is required")
	require.Nil(t, executor)
}

func Test_newDispatcher_NilLogger(t *testing.T) {
	t.Parallel()
	cfg := dispatcherConfig{}
	pipeline := &defaultToolPipeline{}
	observer := &mockLogger{}

	// Explicitly pass nil for the logger
	_, err := newDispatcher(cfg, pipeline, nil, nil, observer)
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
func (m *errorEventBus) Listen(ctx context.Context) error                  { <-ctx.Done(); return ctx.Err() }
func (m *errorEventBus) WaitStarted()                                      {}

func TestDispatcher_EmitEvent_ErrorLogging(t *testing.T) {
	// Setup
	mockLogger := &capturingLogger{}
	genericErr := errors.New("generic publish error")
	mockBus := &errorEventBus{err: genericErr}

	exec := &Dispatcher{
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

func TestDispatcher_EmitEvent_ErrBusNotInitialized_NoLogging(t *testing.T) {
	// Setup
	mockLogger := &capturingLogger{}
	mockBus := &errorEventBus{err: events.ErrBusNotInitialized}

	exec := &Dispatcher{
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

func TestDispatcher_ConsentEvents_DetachedContext(t *testing.T) {
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

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, bus, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	exec.pipeline.(*defaultToolPipeline).authorizer = auth

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

func Test_newDispatcher_DefaultConfig(t *testing.T) {
	cfg := dispatcherConfig{}
	pipeline := &defaultToolPipeline{}
	observer := &mockLogger{}
	logger := &ports.NoOpLogger{}

	executor, err := newDispatcher(cfg, pipeline, nil, logger, observer)
	require.NoError(t, err)
	require.NotNil(t, executor)

	state := executor.state.Load()
	assert.Equal(t, 5, state.config.MaxConcurrentTools)
	assert.Equal(t, 30*time.Second, state.config.ToolTimeout)
	assert.Equal(t, 5*time.Minute, state.config.LongRunningTimeout)
	assert.Equal(t, 5*time.Minute, state.config.ZombieTimeout)
}

func TestRunExecutionPlan_ContextCancellation(t *testing.T) {
	bus := &mockEventBus{}
	logger := &ports.NoOpLogger{}

	slowTool := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		t.Logf("slowTool running")
		select {
		case <-time.After(2 * time.Second):
			return tools.ToolResult{Text: "done"}, nil
		case <-ctx.Done():
			return tools.ToolResult{Text: "cancelled"}, ctx.Err()
		}
	}

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return slowTool(ctx, args, hb)
		},
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			var decs []*tools.ToolDeclaration
			for i := 0; i < 10; i++ {
				decs = append(decs, &tools.ToolDeclaration{Name: fmt.Sprintf("slow_tool_%d", i)})
			}
			return decs
		},
	}

	// We need to bypass the security manager for the test, or the tools will be automatically declined
	sm := &mockSecurityManager{AllowAll: true}

	// Use NewPipelineDispatcher to ensure full pipeline hookup
	exec, err := NewPipelineDispatcher(reg, sm, bus, logger, &mockLogger{})
	require.NoError(t, err)
	exec.SetConcurrency(2)

	content := &llm.Content{}
	for i := 0; i < 10; i++ {
		content.Parts = append(content.Parts, &llm.Part{
			FunctionCall: &llm.FunctionCall{Name: fmt.Sprintf("slow_tool_%d", i)},
		})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Capture initial goroutines
	initialGoroutines := runtime.NumGoroutine()

	start := time.Now()
	_, execErr := exec.Execute(ctx, content, 0, 10)
	duration := time.Since(start)

	// Verify short-circuiting happened
	assert.Error(t, execErr)
	t.Logf("execErr (type: %T): %+v", execErr, execErr)

	if execErr == nil {
		t.Fatalf("execErr is nil")
	}

	if !errors.Is(execErr, llm.ErrTransient) && !errors.Is(execErr, context.DeadlineExceeded) && !errors.Is(execErr, context.Canceled) {
		t.Errorf("expected DeadlineExceeded, Canceled, or ErrTransient, got %v", execErr)
	}
	assert.Less(t, duration, 1*time.Second, "Execution should short-circuit within milliseconds")

	// Poll runtime.NumGoroutine() until it settles within the acceptable delta.
	// This replaces a fixed 100ms sleep with deterministic polling, tolerating
	// slow CI runners while avoiding unnecessary waits on fast machines.
	require.Eventually(t, func() bool {
		return runtime.NumGoroutine() <= initialGoroutines+5
	}, 2*time.Second, 10*time.Millisecond, "goroutines did not settle — possible leak")

	finalGoroutines := runtime.NumGoroutine()
	assert.InDelta(t, initialGoroutines, finalGoroutines, 5, "Number of goroutines should remain stable, proving bounded workers have exited")
}

type mockPanicPipeline struct {
	panicOn string
}

func (m *mockPanicPipeline) RequestBatchConsent(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
	return ctx, nil
}

func (m *mockPanicPipeline) IsSerial(toolName string) bool {
	return false
}

func (m *mockPanicPipeline) ExecuteTool(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
	if call.Name == m.panicOn {
		panic("simulated nil pointer dereference")
	}
	return tools.ToolResult{Text: "success"}
}

func TestRunExecutionPlan_PanicRecovery(t *testing.T) {
	bus := &mockEventBus{}
	logger := &ports.NoOpLogger{}

	pipeline := &mockPanicPipeline{panicOn: "panic_tool"}

	cfg := dispatcherConfig{MaxConcurrentTools: 2}
	exec, err := newDispatcher(cfg, pipeline, bus, logger, &mockLogger{})
	require.NoError(t, err)

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "success_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "panic_tool"}},
		},
	}

	ctx := context.Background()

	// 1. The test suite should not crash (the panic is recovered)
	results := make([]tools.ToolResult, 2)
	calls := []*llm.FunctionCall{content.Parts[0].FunctionCall, content.Parts[1].FunctionCall}
	declinedMap := make(map[int]bool)

	execErr := exec.runExecutionPlan(ctx, calls, declinedMap, results)

	// 2. The recovered panic is returned as an error from runExecutionPlan
	assert.Error(t, execErr)

	// 4. The error string contains "simulated nil pointer dereference"
	assert.Contains(t, execErr.Error(), "simulated nil pointer dereference")

	// 3. The successfully executed tool's result is still handled or acknowledged
	var foundSuccess bool
	var foundPanic bool
	for i, tr := range results {
		switch calls[i].Name {
		case "success_tool":
			assert.Equal(t, "success", tr.Text)
			foundSuccess = true
		case "panic_tool":
			assert.Contains(t, tr.Text, "simulated nil pointer dereference")
			foundPanic = true
		}
	}

	assert.True(t, foundSuccess, "success_tool should have completed successfully")
	assert.True(t, foundPanic, "panic_tool should have completed with panic error")
}

func TestExecutor_PanicRecovery(t *testing.T) {
	t.Parallel()

	// Create a mock registry that panics when a specific tool is executed
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			if name == "panic_tool" {
				panic("simulated fatal error")
			}
			return tools.ToolResult{Text: "success"}, nil
		},
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "panic_tool"}}
		},
	}

	// We need a proper pipeline to test ExecuteTool
	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	call := &llm.FunctionCall{Name: "panic_tool"}

	// Action: Execute the tool which will panic
	result := pipeline.ExecuteTool(context.Background(), call)

	// Assert that we gracefully caught the panic and mapped it to llm.ErrTerminal
	require.Error(t, result.Error)
	assert.ErrorIs(t, result.Error, llm.ErrTerminal, "Panic should be wrapped in llm.ErrTerminal")
	assert.Contains(t, result.Text, "encountered an internal fatal error (panic)")
	assert.Contains(t, result.Error.Error(), "simulated fatal error")
}

func TestExecuteTool_ResolverFailure_ReturnsErrorWithText(t *testing.T) {
	t.Parallel()

	// Registry with no getDeclarationsFn set → defaults to [{Name: "test_tool"}]
	// So "nonexistent_tool" won't be found by the resolver
	reg := &mockToolRegistry{}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "nonexistent_tool"})

	assert.Error(t, result.Error)
	assert.Contains(t, result.Text, "not defined")
	assert.Contains(t, result.Text, "nonexistent_tool")
}

func TestExecuteTool_UserDeclined_ReturnsErrorNilTextHasMessage(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "risky_tool"}}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, tools.ErrUserDeclined
		},
	}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "risky_tool"})

	assert.Nil(t, result.Error, "user_declined must result in Error: nil")
	assert.Contains(t, result.Text, "denied")
}

func TestExecuteTool_SecurityBlocked_ErrNonNil_ReturnsSecurityError(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "blocked_tool"}}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, tools.ErrSecurityPolicy
		},
	}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "blocked_tool"})

	assert.Error(t, result.Error)
	assert.True(t, errors.Is(result.Error, tools.ErrSecurityPolicy))
	assert.Contains(t, result.Text, "blocked")
}

func TestExecuteTool_SecurityBlocked_ResultErrorNonNil_ReturnsSecurityError(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "blocked_tool"}}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{Error: tools.ErrSecurityPolicy, Text: "raw output"}, nil
		},
	}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "blocked_tool"})

	assert.Error(t, result.Error)
	assert.True(t, errors.Is(result.Error, tools.ErrSecurityPolicy))
	assert.Contains(t, result.Text, "blocked")
}

func TestExecuteTool_GenericError_ResultErrorNil_AutoFillsError(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, fmt.Errorf("disk full")
		},
	}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "test_tool"})

	assert.Error(t, result.Error, "Error should be set from err")
	assert.NotEmpty(t, result.Text, "Text should be auto-filled")
	assert.Contains(t, result.Text, "disk full")
}

func TestExecuteTool_GenericError_ResultTextPreserved_DoesNotOverwrite(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "custom error message from tool"}, fmt.Errorf("wrapped failure")
		},
	}

	pipeline := newDefaultToolPipeline(
		reg,
		&mockSecurityManager{AllowAll: true},
		&mockEventBus{},
		&ports.NoOpLogger{},
		nil,
		30*time.Second,
		5*time.Minute,
		5*time.Minute,
	)

	result := pipeline.ExecuteTool(context.Background(), &llm.FunctionCall{Name: "test_tool"})

	assert.Error(t, result.Error, "Error should be auto-filled from err")
	assert.Equal(t, "custom error message from tool", result.Text, "Text must NOT be overwritten")
}

func TestDispatcher_Execute_MaxTurnsExhausted_ReturnsErrMaxTurnsReached(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{}

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	result, execErr := exec.Execute(context.Background(), respContent, 5, 5)

	assert.Nil(t, result, "result should be nil when max turns exhausted")
	assert.Error(t, execErr)
	assert.True(t, errors.Is(execErr, llm.ErrMaxTurnsReached), "error should be ErrMaxTurnsReached")
}

func TestDispatcher_Execute_ErrTerminal_PropagatesErrorWithResponse(t *testing.T) {
	t.Parallel()

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			if name == "fatal_tool" {
				panic("fatal: out of memory")
			}
			return tools.ToolResult{Text: "ok"}, nil
		},
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{{Name: "fatal_tool"}}
		},
	}

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "fatal_tool"}},
		},
	}

	result, execErr := exec.Execute(context.Background(), respContent, 0, 10)

	assert.NotNil(t, result, "result should be the assembled response")
	assert.NotEmpty(t, result.Parts, "result should have at least one part")
	assert.Error(t, execErr)
	assert.True(t, errors.Is(execErr, llm.ErrTerminal), "error should be ErrTerminal")
}

func TestDispatcher_Execute_NonTerminalError_Propagates(t *testing.T) {
	t.Parallel()

	// Define a sentinel error that is NOT terminal.
	errDiskFull := errors.New("disk full")

	mock := &mockToolPipeline{
		IsSerialFunc: func(n string) bool { return false },
		ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
			return tools.ToolResult{
				Text:  "write failed",
				Error: fmt.Errorf("tool failure: %w", errDiskFull),
			}
		},
		RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
			return ctx, nil
		},
	}

	cfg := dispatcherConfig{
		MaxConcurrentTools: 5,
		ToolTimeout:        30 * time.Second,
	}
	cfg.applyDefaults()

	d := &Dispatcher{
		pipeline: mock,
		logger:   &ports.NoOpLogger{},
		strategy: &markdownStrategy{},
	}
	d.state.Store(&dispatcherState{config: cfg})

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "write_file"}},
		},
	}

	result, execErr := d.Execute(context.Background(), respContent, 0, 10)

	// The assembled response must still be returned (with per-tool error details).
	assert.NotNil(t, result, "result should be the assembled response")
	assert.NotEmpty(t, result.Parts, "result should have at least one part")

	// The function-level error must be non-nil — non-terminal errors propagate.
	assert.Error(t, execErr, "non-terminal error must propagate from Execute()")

	// errors.Is must be able to unwrap through errors.Join to find the sentinel.
	assert.True(t, errors.Is(execErr, errDiskFull),
		"errors.Is should match the wrapped non-terminal sentinel error")
}

func TestSuggestTool(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		hallucinated string
		validTools   []string
		want         string
	}{
		{
			name:         "exact match returns tool name",
			hallucinated: "read_file",
			validTools:   []string{"write_file", "read_file", "delete_file"},
			want:         "read_file",
		},
		{
			name:         "close match within distance threshold",
			hallucinated: "read_files",
			validTools:   []string{"write_file", "read_file", "delete_file"},
			want:         "read_file",
		},
		{
			name:         "no match exceeds distance threshold",
			hallucinated: "zzzzzzzzz",
			validTools:   []string{"write_file", "read_file", "delete_file"},
			want:         "",
		},
		{
			name:         "empty valid tools returns empty",
			hallucinated: "anything",
			validTools:   []string{},
			want:         "",
		},
		{
			name:         "case insensitive match",
			hallucinated: "READ_FILE",
			validTools:   []string{"write_file", "read_file", "delete_file"},
			want:         "read_file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SuggestTool(tt.hallucinated, tt.validTools)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestHandleClassifiedError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		status      string
		msg         string
		err         error
		resultErr   error
		wantResult  tools.ToolResult
		wantHandled bool
	}{
		{
			name:        "user_declined returns nil error",
			status:      "user_declined",
			msg:         "User denied action.",
			err:         tools.ErrUserDeclined,
			resultErr:   nil,
			wantResult:  tools.ToolResult{Text: "User denied action.", Error: nil},
			wantHandled: true,
		},
		{
			name:        "security_blocked with err non-nil",
			status:      "security_blocked",
			msg:         "Blocked by policy.",
			err:         tools.ErrSecurityPolicy,
			resultErr:   nil,
			wantResult:  tools.ToolResult{Text: "Blocked by policy.", Error: tools.ErrSecurityPolicy},
			wantHandled: true,
		},
		{
			name:        "security_blocked with resultErr non-nil and err nil",
			status:      "security_blocked",
			msg:         "Blocked by policy.",
			err:         nil,
			resultErr:   tools.ErrSecurityPolicy,
			wantResult:  tools.ToolResult{Text: "Blocked by policy.", Error: tools.ErrSecurityPolicy},
			wantHandled: true,
		},
		{
			name:        "security_blocked with both errors nil falls back to ErrSecurityPolicy",
			status:      "security_blocked",
			msg:         "Blocked by policy.",
			err:         nil,
			resultErr:   nil,
			wantResult:  tools.ToolResult{Text: "Blocked by policy.", Error: tools.ErrSecurityPolicy},
			wantHandled: true,
		},
		{
			name:        "unclassified status error returns not handled",
			status:      "error",
			msg:         "",
			err:         errors.New("some error"),
			resultErr:   nil,
			wantResult:  tools.ToolResult{},
			wantHandled: false,
		},
		{
			name:        "empty status returns not handled",
			status:      "",
			msg:         "",
			err:         nil,
			resultErr:   nil,
			wantResult:  tools.ToolResult{},
			wantHandled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, handled := handleClassifiedError(tt.status, tt.msg, tt.err, tt.resultErr)
			assert.Equal(t, tt.wantHandled, handled)
			assert.Equal(t, tt.wantResult.Text, got.Text)
			if tt.wantResult.Error == nil {
				assert.NoError(t, got.Error)
			} else {
				assert.ErrorIs(t, got.Error, tt.wantResult.Error)
			}
		})
	}
}

func TestAssembleResponse_BinaryData(t *testing.T) {
	t.Parallel()

	calls := []*llm.FunctionCall{
		{ID: "1", Name: "text_tool"},
		{ID: "2", Name: "image_tool"},
	}

	results := []tools.ToolResult{
		{Text: "text output"},
		{
			Text: "image generated",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47}},
				{MIMEType: "image/jpeg", Data: []byte{0xFF, 0xD8}},
			},
		},
	}

	// Create a Dispatcher with the default markdownStrategy
	d := &Dispatcher{strategy: &markdownStrategy{}}

	result := d.AssembleResponse(calls, results)

	assert.Equal(t, "user", result.Role)

	// Expected parts:
	// 1. FunctionResponse for text_tool (from strategy.Format)
	// 2. FunctionResponse for image_tool (from strategy.Format)
	// 3. InlineData for image/png
	// 4. InlineData for image/jpeg
	assert.Len(t, result.Parts, 4)

	// Verify function response parts exist for each call
	var frCount int
	for _, part := range result.Parts {
		if part.FunctionResponse != nil {
			frCount++
		}
	}
	assert.Equal(t, 2, frCount, "expected one FunctionResponse per function call")

	// Verify binary parts are present with correct data
	var binaryCount int
	for _, part := range result.Parts {
		if part.InlineData != nil {
			binaryCount++
			assert.NotEmpty(t, part.InlineData.MIMEType)
			assert.NotEmpty(t, part.InlineData.Data)
		}
	}
	assert.Equal(t, 2, binaryCount, "expected two InlineData parts for the two BinaryData entries")
}

func TestAssembleResponse_NoBinaryData(t *testing.T) {
	t.Parallel()

	calls := []*llm.FunctionCall{
		{ID: "1", Name: "text_tool"},
	}

	results := []tools.ToolResult{
		{Text: "text output", BinaryData: nil},
	}

	d := &Dispatcher{strategy: &markdownStrategy{}}

	result := d.AssembleResponse(calls, results)

	assert.Equal(t, "user", result.Role)
	assert.Len(t, result.Parts, 1, "no InlineData parts when BinaryData is empty")
	assert.NotNil(t, result.Parts[0].FunctionResponse)
	assert.Nil(t, result.Parts[0].InlineData)
}

func TestHandleBatchResults_ContextCancellation_Propagates(t *testing.T) {
	t.Parallel()

	e := &Dispatcher{strategy: &markdownStrategy{}}
	resultsCh := make(chan toolExecResult, 1)
	resultsCh <- toolExecResult{
		index: -1,
		name:  "context_cancelled",
		tr:    tools.ToolResult{Text: "skipped: context cancelled", Error: context.Canceled},
	}
	close(resultsCh)

	results := make([]tools.ToolResult, 1)
	planErrors := e.handleBatchResults(context.Background(), resultsCh, results, nil)

	require.Len(t, planErrors, 1)
	assert.ErrorIs(t, planErrors[0], context.Canceled)
}
