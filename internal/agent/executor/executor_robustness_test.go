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
