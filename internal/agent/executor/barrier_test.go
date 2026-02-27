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
)

func TestToolExecutor_ConcurrentSerialAndParallelTools(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	// Setup tools:
	// - parallel_tool: returns quickly
	// - serial_tool: takes some time

	toolsMap := map[string]toolBehavior{
		"parallel_tool": {
			observe: func() {},
			delay:   10 * time.Millisecond,
			result:  tools.ToolResult{Text: "parallel_ok"},
		},
		"serial_tool": {
			serial:  true,
			observe: func() {},
			delay:   50 * time.Millisecond,
			result:  tools.ToolResult{Text: "serial_ok"},
		},
	}

	t.Run("Parallel Before Serial", func(t *testing.T) {
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "parallel_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
		}}

		startTime := time.Now()
		resp, err := exec.Execute(context.Background(), content, 0, 10)
		duration := time.Since(startTime)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Parts) != 2 {
			t.Fatalf("expected 2 results, got %d", len(resp.Parts))
		}

		// Total duration should be at least delay(parallel) + delay(serial) if they are sequential
		// Actually, even if they are in different batches, they run sequentially.
		// B1: parallel_tool (10ms) -> Wait
		// B2: serial_tool (50ms)
		// Total ~60ms.
		if duration < 60*time.Millisecond {
			t.Logf("Duration: %v (expected ~60ms)", duration)
		}
	})

	t.Run("Serial Before Parallel", func(t *testing.T) {
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "parallel_tool"}},
		}}

		startTime := time.Now()
		resp, err := exec.Execute(context.Background(), content, 0, 10)
		duration := time.Since(startTime)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Parts) != 2 {
			t.Fatalf("expected 2 results, got %d", len(resp.Parts))
		}

		// Current implementation:
		// B1: serial_tool (50ms)
		// B2: parallel_tool (10ms)
		// Total ~60ms.
		if duration < 60*time.Millisecond {
			t.Logf("Duration: %v (expected ~60ms)", duration)
		}
	})

	t.Run("Barrier Pattern - Parallel First", func(t *testing.T) {
		t.Parallel()
	// We want to verify that if we have [Serial, Parallel],
		// the refactored executor runs Parallel FIRST.

		var serialStartedAt time.Time
		var parallelFinishedAt time.Time
		var mu sync.Mutex

		toolsMap := map[string]toolBehavior{
			"p_tool": {
				delay:  20 * time.Millisecond,
				result: tools.ToolResult{Text: "p_ok"},
			},
			"s_tool": {
				serial: true,
				result: tools.ToolResult{Text: "s_ok"},
			},
		}

		exec, _, behaviors := setupTestExecutor(t, toolsMap, nil)
		behaviors["p_tool"].observe = func() {
			time.Sleep(20 * time.Millisecond)
			mu.Lock()
			parallelFinishedAt = time.Now()
			mu.Unlock()
		}
		behaviors["s_tool"].observe = func() {
			mu.Lock()
			serialStartedAt = time.Now()
			mu.Unlock()
		}

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "s_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "p_tool"}},
		}}

		_, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// If Barrier Pattern (Parallel First) is implemented:
		// p_tool runs and finishes.
		// THEN s_tool starts.
		// So serialStartedAt should be AFTER parallelFinishedAt.

		mu.Lock()
		defer mu.Unlock()
		if serialStartedAt.Before(parallelFinishedAt) {
			t.Errorf("Barrier failed: serial tool started at %v, before parallel tool finished at %v", serialStartedAt, parallelFinishedAt)
		} else {
			t.Logf("Barrier success: parallel finished at %v, serial started at %v", parallelFinishedAt, serialStartedAt)
		}
	})
}
