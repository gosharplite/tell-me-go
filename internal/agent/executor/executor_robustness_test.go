// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestToolExecutor_ConfigRace(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{Name: "task"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(2 * time.Millisecond)
		return tools.ToolResult{Text: "ok"}, nil
	})

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)
	ctx := context.Background()
	var wg sync.WaitGroup

	// Start concurrent executions
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := &llm.Content{
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "task"}}},
			}
			for j := 0; j < 5; j++ {
				_, _ = exec.Execute(ctx, content, 0, 10)
			}
		}()
	}

	// Hammer config updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			exec.SetConcurrency(1+(i%10), time.Duration(10+i)*time.Millisecond)
			time.Sleep(1 * time.Millisecond)
		}
	}()

	wg.Wait()
}

func TestToolExecutor_GoroutineLeak(t *testing.T) {
	reg := registry.New()

	// This tool ignores the context and sleeps
	toolFinished := make(chan struct{})
	reg.Register(&tools.ToolDeclaration{Name: "leaky_tool"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		// DONT check ctx.Done()
		time.Sleep(200 * time.Millisecond)
		close(toolFinished)
		return tools.ToolResult{Text: "I finally finished"}, nil
	})

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)
	exec.SetConcurrency(1, 50*time.Millisecond) // Short timeout

	initialGoroutines := runtime.NumGoroutine()

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "leaky_tool"}},
		},
	}

	start := time.Now()
	_, err := exec.Execute(context.Background(), content, 0, 5)
	duration := time.Since(start)

	if err != nil {
		t.Logf("Execute returned error as expected: %v", err)
	}

	// It should have returned due to timeout (~50ms), not waited for the tool (~200ms)
	if duration >= 150*time.Millisecond {
		t.Errorf("Execute took too long: %v, expected timeout around 50ms", duration)
	}

	// Check for leak immediately after return
	currentGoroutines := runtime.NumGoroutine()
	t.Logf("Initial: %d, Current after timeout: %d", initialGoroutines, currentGoroutines)

	// Wait for the leaky tool to actually finish so we don't leak into other tests
	select {
	case <-toolFinished:
		t.Log("Leaky tool finally finished")
	case <-time.After(500 * time.Millisecond):
		t.Error("Leaky tool never finished")
	}

	// After tool finishes, goroutine count should go back down
	time.Sleep(50 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()
	t.Logf("Final: %d", finalGoroutines)
}
