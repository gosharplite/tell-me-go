// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

type mockToolRegistry struct {
	executeFn func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)
	isSerial  bool
}

func (m *mockToolRegistry) GetDeclarations() []*tools.ToolDeclaration {
	return []*tools.ToolDeclaration{{Name: "test_tool"}}
}
func (m *mockToolRegistry) Register(d *tools.ToolDeclaration, f tools.ToolFunc) {}
func (m *mockToolRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args)
	}
	return tools.ToolResult{Text: "ok"}, nil
}
func (m *mockToolRegistry) IsSerial(name string) bool      { return m.isSerial }
func (m *mockToolRegistry) IsLongRunning(name string) bool { return false }

func TestToolExecutor_ContextCancellation(t *testing.T) {
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

	exec := NewToolExecutor(reg, nil, nil)
	defer exec.Shutdown()

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
	pool := NewWorkerPool(1)
	defer pool.Shutdown()

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
