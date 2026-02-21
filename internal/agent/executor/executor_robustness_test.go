// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
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

func TestToolExecutor_ContextCancellation_MidBatch(t *testing.T) {
	reg := registry.New()
	
	// Create a tool that blocks until told to proceed, so we can reliably cancel context mid-batch
	blockCh := make(chan struct{})
	reg.Register(&tools.ToolDeclaration{Name: "blocking_tool"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		select {
		case <-blockCh:
			return tools.ToolResult{Text: "ok"}, nil
		case <-ctx.Done():
			return tools.ToolResult{Text: "canceled", Error: ctx.Err()}, nil
		}
	})

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)

	ctx, cancel := context.WithCancel(context.Background())

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "blocking_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "blocking_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "blocking_tool"}},
		},
	}

	var wg sync.WaitGroup
	wg.Add(1)
	
	var execErr error
	go func() {
		defer wg.Done()
		_, execErr = exec.Execute(ctx, content, 0, 10)
	}()

	// Wait a moment for the executor to enqueue the tools and block
	time.Sleep(50 * time.Millisecond)

	// Cancel the context mid-batch
	cancel()
	
	// Unblock the tools
	close(blockCh)

	wg.Wait()

	if execErr == nil {
		t.Fatalf("expected context canceled error, got nil")
	}

	if !strings.Contains(execErr.Error(), "canceled") {
		t.Fatalf("expected context canceled error, got %v", execErr)
	}
}
