// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestDispatcher_ConcurrentSerialAndParallelTools(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}

	toolsMap := map[string]testutil.ToolBehavior{
		"parallel_tool": {
			Observe: func() {},
			Result:  tools.ToolResult{Text: "parallel_ok"},
		},
		"serial_tool": {
			Serial:  true,
			Observe: func() {},
			Result:  tools.ToolResult{Text: "serial_ok"},
		},
	}

	t.Run("Parallel Before Serial", func(t *testing.T) {
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "parallel_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionResponse(t, resp, err, 2)
	})

	t.Run("Serial Before Parallel", func(t *testing.T) {
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "parallel_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionResponse(t, resp, err, 2)
	})

	t.Run("Sequential Integrity - Serial First", func(t *testing.T) {
		t.Parallel()

		serialFinished := make(chan struct{})
		var parallelStartedAfterSerial bool
		var mu sync.Mutex

		toolsMap := map[string]testutil.ToolBehavior{
			"p_tool": {
				Result: tools.ToolResult{Text: "p_ok"},
			},
			"s_tool": {
				Serial: true,
				Result: tools.ToolResult{Text: "s_ok"},
			},
		}

		exec, _, behaviors := setupTestExecutor(t, toolsMap, nil)
		behaviors["s_tool"].Observe = func() {
			close(serialFinished)
		}
		behaviors["p_tool"].Observe = func() {
			select {
			case <-serialFinished:
				mu.Lock()
				parallelStartedAfterSerial = true
				mu.Unlock()
			default:
				// If serialFinished is not closed, parallel tool started too early
			}
		}

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "s_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "p_tool"}},
		}}

		_, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		mu.Lock()
		defer mu.Unlock()
		assertSequentialIntegrity(t, parallelStartedAfterSerial)
	})
}

func assertExecutionResponse(t *testing.T, resp *llm.Content, err error, expectedParts int) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil || len(resp.Parts) != expectedParts {
		t.Fatalf("expected %d results, got %v", expectedParts, resp)
	}
}

func assertSequentialIntegrity(t *testing.T, parallelStartedAfterSerial bool) {
	t.Helper()
	if !parallelStartedAfterSerial {
		t.Errorf("Sequential Integrity failed: parallel tool started before serial tool finished")
	}
}
