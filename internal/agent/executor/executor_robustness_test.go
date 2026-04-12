// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_ConfigRace(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow robustness test in short mode")
	}
	reg := registry.New()
	err := reg.Register(&tools.ToolDeclaration{Name: "task"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		runtime.Gosched()
		return tools.ToolResult{Text: "ok"}, nil
	})
	require.NoError(t, err)

	exec, err := executor.NewPipelineDispatcher(reg, &executor.MockSecurityManager{AllowAll: true}, &executor.MockEventBus{}, &ports.NoOpLogger{}, &executor.MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	ctx := context.Background()
	var wg sync.WaitGroup

	// Start sequential execution (Dispatcher handles one turn per session)
	wg.Add(1)
	go func() {
		defer wg.Done()
		content := &llm.Content{
			Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "task"}}},
		}
		for j := 0; j < 20; j++ {
			_, _ = exec.Execute(ctx, content, 0, 10)
		}
	}()

	// Hammer config updates
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			exec.SetConcurrency(1 + (i % 10))
			runtime.Gosched()
		}
	}()

	wg.Wait()
}

func TestDispatcher_ContextCancellation_MidBatch(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow robustness test in short mode")
	}
	reg := registry.New()

	// Create a tool that blocks until told to proceed, so we can reliably cancel context mid-batch
	blockCh := make(chan struct{})
	toolStarted := make(chan struct{}, 1)
	regErr := reg.Register(&tools.ToolDeclaration{Name: "blocking_tool"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		select {
		case toolStarted <- struct{}{}:
		default:
		}
		select {
		case <-blockCh:
			return tools.ToolResult{Text: "ok"}, nil
		case <-ctx.Done():
			return tools.ToolResult{Text: "canceled", Error: ctx.Err()}, nil
		}
	})
	require.NoError(t, regErr)

	exec, err := executor.NewPipelineDispatcher(reg, &executor.MockSecurityManager{AllowAll: true}, &executor.MockEventBus{}, &ports.NoOpLogger{}, &executor.MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

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

	// Wait for the executor to start running the tool
	<-toolStarted

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
