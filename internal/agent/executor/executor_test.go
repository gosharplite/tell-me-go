// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockToolRegistry struct {
	executeFn         func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)
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
func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args)
	}
	return tools.ToolResult{Text: "ok"}, nil
}
func (m *mockToolRegistry) IsSerial(name string) bool      { return m.isSerial }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }

func TestToolExecutor_ContextCancellation(t *testing.T) {
	t.Parallel()
	// Setup a tool that blocks until context is cancelled

	toolStarted := make(chan struct{})
	toolFinished := make(chan struct{})

	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			close(toolStarted)
			select {
			case <-ctx.Done():
				close(toolFinished)
				return tools.ToolResult{}, ctx.Err()
			case <-time.After(2 * time.Second):
				return tools.ToolResult{Text: "timeout"}, nil
			}
		},
	}

	exec, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
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
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("Execute did not return after context cancellation")
	}

	assert.Error(t, execErr)
	assert.Equal(t, context.Canceled, execErr)

	// Verify tool also finished
	select {
	case <-toolFinished:
		// Success
	case <-time.After(1 * time.Second):
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

	pool.Submit(task)
	<-started

	cancel() // Cancel the task context

	select {
	case <-finished:
		// Worker should be free now
	case <-time.After(100 * time.Millisecond):
		t.Error("Worker did not release after task context cancellation")
	}

	// Pool should still be functional for other tasks
	task2Started := make(chan struct{})
	pool.Submit(func(ctx context.Context) {
		close(task2Started)
	})

	select {
	case <-task2Started:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Error("Worker pool became unresponsive after task cancellation")
	}
}

func TestExecuteParallelBatch_ContextCancellation(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	exec, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	batch := taskBatch{
		isSerial: false,
		tasks:    []int{0},
	}
	calls := []*llm.FunctionCall{
		{Name: "test_tool"},
	}
	resChan := make(chan toolExecResult, 1)

	exec.executeParallelBatch(ctx, batch, calls, resChan)

	select {
	case res := <-resChan:
		assert.Equal(t, 0, res.index)
		assert.Equal(t, "test_tool", res.name)
		assert.ErrorContains(t, res.tr.Error, "batch interrupted")
	case <-time.After(1 * time.Second):
		t.Fatal("Expected result on resChan, but got none")
	}
}

func TestBuildExecutionBatches_PreservesOrder(t *testing.T) {
	t.Parallel()
	reg := &orderMockRegistry{
		serialTools: map[string]bool{
			"S1": true,
			"S2": true,
		},
	}
	exec, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{})
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	calls := []*llm.FunctionCall{
		{Name: "P1"},
		{Name: "S1"},
		{Name: "P2"},
		{Name: "P3"},
		{Name: "S2"},
	}

	resChan := make(chan toolExecResult, len(calls))
	batches := exec.buildExecutionBatches(calls, nil, resChan)

	// Expected batches:
	// 1. Parallel: [P1] (index 0)
	// 2. Serial: [S1] (index 1)
	// 3. Parallel: [P2, P3] (index 2, 3)
	// 4. Serial: [S2] (index 4)

	assert.Equal(t, 4, len(batches), "Should have 4 batches")

	assert.False(t, batches[0].isSerial)
	assert.Equal(t, []int{0}, batches[0].tasks)

	assert.True(t, batches[1].isSerial)
	assert.Equal(t, []int{1}, batches[1].tasks)

	assert.False(t, batches[2].isSerial)
	assert.Equal(t, []int{2, 3}, batches[2].tasks)

	assert.True(t, batches[3].isSerial)
	assert.Equal(t, []int{4}, batches[3].tasks)
}

type orderMockRegistry struct {
	serialTools map[string]bool
}

func (m *orderMockRegistry) GetDeclarations() []*tools.ToolDeclaration                 { return nil }
func (m *orderMockRegistry) Register(d *tools.ToolDeclaration, f tools.ToolFunc) error { return nil }
func (m *orderMockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}
func (m *orderMockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}
func (m *orderMockRegistry) IsSerial(name string) bool      { return m.serialTools[name] }
func (m *orderMockRegistry) IsLongRunning(name string) bool { return false }

func TestToolExecutor_PoolClosed_FailsGracefully(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{}
	exec, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
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

func TestToolExecutor_WithActiveTrace_RecordsExecution(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "tool success"}, nil
		},
	}
	exec, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
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

func TestNewToolExecutor_NilObserver(t *testing.T) {
	reg := &mockToolRegistry{}
	_, err := NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, nil)
	require.Error(t, err)
	assert.Equal(t, "ExecutionObserver is required", err.Error())

	// Coverage for lines 95-97: error from NewZombieTool
	sabotageOpt := func(e *ToolExecutor) {
		e.observer = nil
	}
	_, err = NewToolExecutor(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{}, sabotageOpt)
	require.Error(t, err)
	assert.Equal(t, "ExecutionObserver is required", err.Error())
}

func TestNewToolExecutor_NilRegistry(t *testing.T) {
	// Call with nil registry
	executor, err := NewToolExecutor(nil, nil, nil, &ports.NoOpLogger{}, &MockLogger{})

	// Should return an error and a nil executor
	require.Error(t, err)
	require.Contains(t, err.Error(), "registry is required")
	require.Nil(t, executor)
}

func TestNewToolExecutor_NilLogger_FallbackBehavior(t *testing.T) {
	t.Parallel()
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "success"}, nil
		},
	}
	observer := &MockLogger{}

	// Explicitly pass nil for the logger
	exec, err := NewToolExecutor(reg, nil, nil, nil, observer)
	require.NoError(t, err)
	require.NotNil(t, exec)
	t.Cleanup(exec.Shutdown)

	// Call a public method that triggers logging
	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	// This should not panic because e.logger should have defaulted to NoOpLogger
	assert.NotPanics(t, func() {
		results, err := exec.Execute(context.Background(), respContent, 0, 10)
		assert.NoError(t, err)
		assert.NotNil(t, results)
	})
}
