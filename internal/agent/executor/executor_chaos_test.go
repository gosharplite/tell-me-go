// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestToolPanicSerial(t *testing.T) {
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			panic("simulated serial tool panic")
		},
		isSerial: true,
	}

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: "test_tool"}
	resChan := make(chan toolExecResult, 1)

	// executeSerialTask returns bool (true if it can continue)
	canContinue := exec.executeSerialTask(ctx, 0, fc, resChan)

	assert.False(t, canContinue, "Should not continue after panic")

	select {
	case res := <-resChan:
		assert.Equal(t, 0, res.index)
		assert.Contains(t, res.tr.Text, "encountered an internal fatal error (panic)")
		assert.Error(t, res.tr.Error)
		assert.Contains(t, res.tr.Error.Error(), "Panic detected: simulated serial tool panic")
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

func TestToolPanicParallel(t *testing.T) {
	reg := &mockToolRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			if name == "panic_tool" {
				panic("simulated parallel tool panic")
			}
			return tools.ToolResult{Text: "ok"}, nil
		},
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "panic_tool"},
				{Name: "normal_tool"},
			}
		},
		isSerial: false,
	}

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	calls := []*llm.FunctionCall{
		{Name: "panic_tool"},
		{Name: "normal_tool"},
	}
	resChan := make(chan toolExecResult, 2)
	batch := taskBatch{
		isSerial: false,
		tasks:    []int{0, 1},
	}

	exec.executeParallelBatch(ctx, batch, calls, resChan)

	results := make(map[string]toolExecResult)
	for i := 0; i < 2; i++ {
		select {
		case res := <-resChan:
			results[res.name] = res
		case <-time.After(1 * time.Second):
			t.Fatal("Timeout waiting for results")
		}
	}

	panicRes := results["panic_tool"]
	assert.Contains(t, panicRes.tr.Text, "encountered an internal fatal error (panic)")
	assert.Error(t, panicRes.tr.Error)
	assert.Contains(t, panicRes.tr.Error.Error(), "Panic detected: simulated parallel tool panic")

	normalRes := results["normal_tool"]
	assert.Equal(t, "ok", normalRes.tr.Text)
	assert.NoError(t, normalRes.tr.Error)
}

func TestAuthorizationPanic(t *testing.T) {
	// Mock registry where resolveTool or something else inside identifyConsentItems panics
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			panic("simulated auth panic")
		},
	}

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	calls := []*llm.FunctionCall{
		{Name: "panic_auth_tool"},
	}

	// identifyConsentItems has a recover block that marks it as declined
	consentIndices, declinedMap := exec.identifyConsentItems(calls)

	assert.Empty(t, consentIndices, "Should not have consent indices if it panicked")
	assert.True(t, declinedMap[0], "Tool should be marked as declined/denied after panic (fail closed)")
}

func TestZombieToolTimeout(t *testing.T) {
	hangingTool := &tools.ToolDeclaration{
		Name: "zombie_tool",
	}

	stopCh := make(chan struct{})
	// Mock registry that blocks indefinitely until stopCh is closed
	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			<-stopCh // Block until we release it
			return tools.ToolResult{Text: "finally done"}, nil
		},
	}

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	// Set very short timeouts for testing
	exec.toolTimeout = 10 * time.Millisecond
	exec.zombieTimeout = 20 * time.Millisecond

	ctx := context.Background()

	start := time.Now()
	result, err := exec.runWithTimeout(ctx, hangingTool, nil)
	duration := time.Since(start)

	assert.NoError(t, err)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "timed out")
	assert.True(t, duration >= 10*time.Millisecond)

	// Wait for zombie monitor to fire
	time.Sleep(50 * time.Millisecond)

	// Clean up the zombie goroutine so goleak doesn't complain
	close(stopCh)
}

func TestExecuteSerialTaskRecovery(t *testing.T) {
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			panic("panic during serial resolve")
		},
	}
	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: "any_tool"}
	resChan := make(chan toolExecResult, 1)

	exec.executeSerialTask(ctx, 0, fc, resChan)

	select {
	case res := <-resChan:
		assert.Contains(t, res.tr.Text, "encountered an internal fatal error (panic)")
		assert.Contains(t, res.tr.Error.Error(), "panic during serial resolve")
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}

func TestEnqueueParallelTaskRecovery(t *testing.T) {
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			panic("panic during parallel resolve")
		},
	}
	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: "any_tool"}
	resChan := make(chan toolExecResult, 1)
	var wg sync.WaitGroup

	exec.enqueueParallelTask(ctx, 0, fc, resChan, &wg)
	wg.Wait()

	select {
	case res := <-resChan:
		assert.Contains(t, res.tr.Text, "encountered an internal fatal error (panic)")
		assert.Contains(t, res.tr.Error.Error(), "panic during parallel resolve")
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for result")
	}
}
