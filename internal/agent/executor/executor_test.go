// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestToolExecutor_PanicRecovery(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		panic("intentional parallel panic")
	})
	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "serial_panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		panic("intentional serial panic")
	}, registry.ToolOptions{Serial: true})

	sm := security.NewSecurityManager(nil)
	bus := &events.SimpleEventBus{}
	exec := NewToolExecutor(reg, sm, bus)
	exec.SetConcurrency(2, 0)

	t.Run("Parallel Panic", func(t *testing.T) {
		calls := []*llm.FunctionCall{
			{Name: "panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		exec.runExecutionPlan(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional parallel panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})

	t.Run("Serial Panic", func(t *testing.T) {
		calls := []*llm.FunctionCall{
			{Name: "serial_panic_tool"},
		}

		resChan := make(chan toolExecResult, len(calls))
		exec.runExecutionPlan(context.Background(), calls, resChan)

		res := <-resChan
		if !strings.Contains(res.tr.Text, "Panic detected: intentional serial panic") {
			t.Errorf("expected panic error message, got: %s", res.tr.Text)
		}
	})
}

func TestResultCollector(t *testing.T) {
	t.Parallel()
	calls := []*llm.FunctionCall{
		{Name: "tool0"},
		{Name: "tool1"},
		{Name: "tool2"},
	}

	t.Run("Ordering", func(t *testing.T) {
		collector := newResultCollector(calls, nil)
		// Simulate tools finishing out of order
		collector.ch <- toolExecResult{index: 2, name: "tool2", tr: tools.ToolResult{Text: "res2"}}
		collector.ch <- toolExecResult{index: 0, name: "tool0", tr: tools.ToolResult{Text: "res0"}}
		collector.ch <- toolExecResult{index: 1, name: "tool1", tr: tools.ToolResult{Text: "res1"}}

		results, err := collector.Wait(context.Background())
		if err != nil {
			t.Fatal(err)
		}

		if len(results) != 3 {
			t.Fatalf("Expected 3 results, got %d", len(results))
		}
		if results[0].Text != "res0" || results[1].Text != "res1" || results[2].Text != "res2" {
			t.Errorf("Results out of order: %v", results)
		}
	})

	t.Run("Context Cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		collector := newResultCollector(calls, nil)
		cancel()

		_, err := collector.Wait(ctx)
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}

type MockStrategy struct{}

func (s *MockStrategy) Format(name string, result tools.ToolResult) *llm.Part {
	return &llm.Part{
		FunctionResponse: &llm.FunctionResponse{
			Name:     name,
			Response: map[string]interface{}{"result": result.Text},
		},
	}
}

func TestToolExecutor_AssembleResponse_Binary(t *testing.T) {
	t.Parallel()
	e := &ToolExecutor{strategy: &MockStrategy{}}

	largeBlob := make([]byte, 5*1024*1024) // 5MB
	for i := range largeBlob {
		largeBlob[i] = byte(i % 256)
	}

	tests := []struct {
		name      string
		calls     []*llm.FunctionCall
		results   []tools.ToolResult
		wantParts int
	}{
		{
			name:  "Single Tool with Binary",
			calls: []*llm.FunctionCall{{Name: "get_image"}},
			results: []tools.ToolResult{{
				Text: "Here is your image",
				BinaryData: []tools.BinaryData{
					{MIMEType: "image/png", Data: largeBlob},
				},
			}},
			wantParts: 2, // 1 Text + 1 Binary
		},
		{
			name:  "Multiple Binary Parts",
			calls: []*llm.FunctionCall{{Name: "get_files"}},
			results: []tools.ToolResult{{
				BinaryData: []tools.BinaryData{
					{MIMEType: "application/pdf", Data: []byte{1, 2, 3}},
					{MIMEType: "text/plain", Data: []byte{4, 5, 6}},
				},
			}},
			wantParts: 3, // 1 Text (from Strategy.Format) + 2 Binary
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			content := e.assembleResponse(tt.calls, tt.results)

			if len(content.Parts) != tt.wantParts {
				t.Errorf("Got %d parts, want %d", len(content.Parts), tt.wantParts)
			}

			// Verify the last part of the first result is our large blob
			lastPart := content.Parts[len(content.Parts)-1]
			if lastPart.InlineData != nil {
				if tt.name == "Single Tool with Binary" && !bytes.Equal(lastPart.InlineData.Data, largeBlob) {
					t.Error("Binary data corruption detected")
				}
			}
		})
	}
}

func TestToolExecutor_SerialTimeoutHalt(t *testing.T) {
	reg := registry.New()
	// slow_tool is Serial and will wait until context is cancelled
	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		<-ctx.Done()
		return tools.ToolResult{Text: "Finished eventually"}, ctx.Err()
	}, registry.ToolOptions{Serial: true})

	// fast_tool should be skipped if slow_tool times out
	fastExecuted := false
	reg.Register(&tools.ToolDeclaration{
		Name: "fast_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		fastExecuted = true
		return tools.ToolResult{Text: "Fast result"}, nil
	})

	sm := security.NewSecurityManager(nil)
	bus := &events.SimpleEventBus{}
	exec := NewToolExecutor(reg, sm, bus)
	exec.SetConcurrency(2, 100*time.Millisecond) // Short timeout for tools

	calls := []*llm.FunctionCall{
		{Name: "slow_tool"},
		{Name: "fast_tool"},
	}

	resChan := make(chan toolExecResult, len(calls))
	exec.runExecutionPlan(context.Background(), calls, resChan)

	// Collect results
	results := make([]toolExecResult, len(calls))
	for i := 0; i < len(calls); i++ {
		results[i] = <-resChan
	}

	// Verify slow_tool timed out
	if results[0].name != "slow_tool" {
		t.Errorf("expected slow_tool, got %s", results[0].name)
	}
	if results[0].tr.Error != context.DeadlineExceeded {
		t.Errorf("expected context.DeadlineExceeded, got %v", results[0].tr.Error)
	}

	// Verify fast_tool was skipped
	if results[1].name != "fast_tool" {
		t.Errorf("expected fast_tool, got %s", results[1].name)
	}
	if !strings.Contains(results[1].tr.Text, "Skipped: Execution halted") {
		t.Errorf("expected skipped message, got %s", results[1].tr.Text)
	}
	if fastExecuted {
		t.Error("fast_tool was executed but should have been skipped")
	}
}

func TestToolExecutor_SerialPanicHalt(t *testing.T) {
	reg := registry.New()
	reg.RegisterWithOptions(&tools.ToolDeclaration{
		Name: "panic_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		panic("boom")
	}, registry.ToolOptions{Serial: true})

	fastExecuted := false
	reg.Register(&tools.ToolDeclaration{
		Name: "fast_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		fastExecuted = true
		return tools.ToolResult{Text: "Fast result"}, nil
	})

	sm := security.NewSecurityManager(nil)
	bus := &events.SimpleEventBus{}
	exec := NewToolExecutor(reg, sm, bus)

	calls := []*llm.FunctionCall{
		{Name: "panic_tool"},
		{Name: "fast_tool"},
	}

	resChan := make(chan toolExecResult, len(calls))
	exec.runExecutionPlan(context.Background(), calls, resChan)

	results := make([]toolExecResult, len(calls))
	for i := 0; i < len(calls); i++ {
		results[i] = <-resChan
	}

	if !strings.Contains(results[0].tr.Text, "Panic detected: boom") {
		t.Errorf("expected panic message, got %s", results[0].tr.Text)
	}
	if results[0].tr.Error == nil {
		t.Error("expected non-nil Error on panic")
	}

	if !strings.Contains(results[1].tr.Text, "Skipped: Execution halted") {
		t.Errorf("expected skipped message for subsequent tool, got %s", results[1].tr.Text)
	}
	if fastExecuted {
		t.Error("fast_tool was executed after a serial panic")
	}
}
