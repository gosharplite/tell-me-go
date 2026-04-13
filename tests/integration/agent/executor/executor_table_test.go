// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"errors"
	"reflect"
	"strings"

	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
	"github.com/stretchr/testify/require"
)

func setupTestRegistry(t *testing.T, toolsMap map[string]testutil.ToolBehavior) (tools.Registry, map[string]*testutil.ToolBehavior) {
	t.Helper()
	reg := testutil.NewMockToolRegistry()
	behaviors := make(map[string]*testutil.ToolBehavior)
	for name, behavior := range toolsMap {
		b := behavior
		behaviors[name] = &b
		opts := tools.ToolOptions{
			Serial:      b.Serial,
			LongRunning: b.Long,
		}
		if err := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: name}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			if b.Observe != nil {
				b.Observe()
			}
			if b.Panic != nil {
				panic(b.Panic)
			}
			if b.Delay > 0 {
				timer := time.NewTimer(b.Delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				case <-timer.C:
				}
			}
			return b.Result, b.Err
		}, opts); err != nil {
			t.Fatalf("failed to register tool %s: %v", name, err)
		}
	}
	return reg, behaviors
}

func setupMockSecurityManager(allowedTools []string) *testutil.MockSecurityManager {
	if allowedTools != nil {
		sm := &testutil.MockSecurityManager{
			AllowedCommands: make(map[string]bool),
		}
		for _, tool := range allowedTools {
			sm.AllowedCommands[tool] = true
		}
		return sm
	}
	return &testutil.MockSecurityManager{AllowAll: true}
}

func setupTestExecutor(t *testing.T, toolsMap map[string]testutil.ToolBehavior, allowedTools []string, opts ...executor.ExecutorOption) (*executor.Dispatcher, *testutil.MockEventBus, map[string]*testutil.ToolBehavior) {
	reg, behaviors := setupTestRegistry(t, toolsMap)
	sm := setupMockSecurityManager(allowedTools)

	bus := &testutil.MockEventBus{}
	exec, err := executor.NewPipelineDispatcher(reg, sm, bus, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)}, opts...)
	require.NoError(t, err)

	return exec, bus, behaviors
}

func assertExecutionSuccess(t *testing.T, resp *llm.Content, err error, expectedResults ...string) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp == nil || len(resp.Parts) != len(expectedResults) {
		t.Fatalf("expected %d response parts, got %v", len(expectedResults), resp)
	}

	for i, expected := range expectedResults {
		if resp.Parts[i].FunctionResponse == nil {
			t.Fatalf("part %d: expected FunctionResponse, got %v", i, resp.Parts[i])
		}
		res := resp.Parts[i].FunctionResponse.Response["result"].(string)
		if res != expected {
			t.Errorf("part %d: unexpected Result: %s, want %s", i, res, expected)
		}
	}
}

func assertExecutionError(t *testing.T, resp *llm.Content, err error, bus *testutil.MockEventBus, expectedMsg string, expectedErr error) {
	t.Helper()
	if expectedErr != nil && errors.Is(expectedErr, llm.ErrTerminal) {
		if err == nil {
			t.Fatalf("expected terminal error, got nil")
		} else if !errors.Is(err, llm.ErrTerminal) {
			t.Fatalf("expected error to wrap llm.ErrTerminal, got: %v", err)
		}
	} else {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	verifyErrorResponse(t, resp, expectedMsg)
	if bus != nil && expectedErr != nil {
		verifyToolEventError(t, bus, expectedErr)
	}
}

func verifyErrorResponse(t *testing.T, resp *llm.Content, expectedMsg string) {
	t.Helper()
	if resp == nil || len(resp.Parts) == 0 {
		t.Fatalf("expected response parts, got %v", resp)
	}
	res := resp.Parts[0].FunctionResponse.Response["result"].(string)
	if !strings.Contains(res, expectedMsg) {
		t.Errorf("expected error message containing %q, got %q", expectedMsg, res)
	}
}

func verifyToolEventError(t *testing.T, bus *testutil.MockEventBus, expectedErr error) {
	t.Helper()
	evs := bus.FilterEvents(reflect.TypeOf(events.ToolResultEvent{}))
	if len(evs) == 0 {
		t.Fatalf("expected ToolResultEvent to be published")
	}
	if !errors.Is(evs[0].(events.ToolResultEvent).Result.Error, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, evs[0].(events.ToolResultEvent).Result.Error)
	}
}

func TestDispatcher_Success(t *testing.T) {
	t.Parallel()

	t.Run("Parallel Execution", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"tool1": {Result: tools.ToolResult{Text: "res1"}},
			"tool2": {Result: tools.ToolResult{Text: "res2"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "tool1"}},
			{FunctionCall: &llm.FunctionCall{Name: "tool2"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionSuccess(t, resp, err, "res1", "res2")
	})

	t.Run("Sequential Execution", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"serial_tool": {Result: tools.ToolResult{Text: "serial_res"}, Serial: true},
			"tool2":       {Result: tools.ToolResult{Text: "res2"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "tool2"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionSuccess(t, resp, err, "serial_res", "res2")
	})
}

func TestDispatcher_Errors(t *testing.T) {
	t.Parallel()

	t.Run("Tool Not Found", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"existing": {Result: tools.ToolResult{Text: "ok"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "missing"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool \"missing\" is not defined")
	})

	t.Run("Tool Suggestion", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"list_files": {Result: tools.ToolResult{Text: "ok"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "list_file"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		verifyErrorResponse(t, resp, "Did you mean \"list_files\"?")
	})

	t.Run("Security Violation", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"forbidden": {Result: tools.ToolResult{Text: "ok"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, []string{"allowed"})
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "forbidden"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, bus, "Action blocked by the system sandbox security policy", tools.ErrSecurityPolicy)
	})

	t.Run("Tool Returns Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"fail_tool": {Err: errors.New("tool failed")},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "fail_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		verifyErrorResponse(t, resp, "Error: tool execution failed: fail_tool: tool failed")
	})
}

func TestDispatcher_SafetyLimits(t *testing.T) {
	t.Parallel()

	t.Run("Tool Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"slow": {Delay: 500 * time.Millisecond, Result: tools.ToolResult{Text: "too late"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil, executor.WithToolTimeout(50*time.Millisecond))

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "slow"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, bus, "Tool execution timed out", llm.ErrTransient)
	})

	t.Run("Max Turns Reached", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"tool": {Result: tools.ToolResult{Text: "ok"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "tool"}},
		}}

		_, err := exec.Execute(context.Background(), content, 5, 5)
		if !errors.Is(err, llm.ErrMaxTurnsReached) {
			t.Fatalf("expected ErrMaxTurnsReached, got %v", err)
		}
	})

	t.Run("Long Running Tool - Extended Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"long_tool": {Delay: 100 * time.Millisecond, Result: tools.ToolResult{Text: "finally finished"}, Long: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil, executor.WithToolTimeout(50*time.Millisecond))

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "long_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Parts) == 0 {
			t.Fatalf("expected response parts")
		}
		res := resp.Parts[0].FunctionResponse.Response["result"].(string)
		if res != "finally finished" {
			t.Errorf("expected 'finally finished', got %s", res)
		}
	})
}

func TestDispatcher_PanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Parallel Panic", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"panic_tool": {Panic: "kaboom"},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "panic_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, bus, "Tool \"panic_tool\" encountered an internal fatal error (panic) and was terminated.", llm.ErrTerminal)
	})

	t.Run("Serial Panic", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"serial_panic": {Panic: "serial kaboom", Serial: true},
			"next_serial":  {Result: tools.ToolResult{Text: "should skip"}, Serial: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_panic"}},
			{FunctionCall: &llm.FunctionCall{Name: "next_serial"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err == nil || !errors.Is(err, llm.ErrTerminal) {
			t.Fatalf("expected terminal error, got: %v", err)
		}
		if resp == nil || len(resp.Parts) < 2 {
			t.Fatalf("expected at least 2 parts, got %v", resp)
		}
		res0 := resp.Parts[0].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res0, "Tool \"serial_panic\" encountered an internal fatal error (panic) and was terminated.") {
			t.Errorf("expected serial panic error, got %s", res0)
		}
		res1 := resp.Parts[1].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res1, "Skipped: Execution halted") {
			t.Errorf("expected skipped message, got %s", res1)
		}
	})
}

func TestDispatcher_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("Concurrency Limit", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"t1": {Delay: 100 * time.Millisecond, Result: tools.ToolResult{Text: "r1"}},
			"t2": {Delay: 100 * time.Millisecond, Result: tools.ToolResult{Text: "r2"}},
			"t3": {Delay: 100 * time.Millisecond, Result: tools.ToolResult{Text: "r3"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "t1"}},
			{FunctionCall: &llm.FunctionCall{Name: "t2"}},
			{FunctionCall: &llm.FunctionCall{Name: "t3"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionSuccess(t, resp, err, "r1", "r2", "r3")
	})

	t.Run("Mixed Path - Parallel then Serial", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"p1": {Result: tools.ToolResult{Text: "pr1"}},
			"p2": {Result: tools.ToolResult{Text: "pr2"}},
			"s1": {Result: tools.ToolResult{Text: "sr1"}, Serial: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "p1"}},
			{FunctionCall: &llm.FunctionCall{Name: "p2"}},
			{FunctionCall: &llm.FunctionCall{Name: "s1"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionSuccess(t, resp, err, "pr1", "pr2", "sr1")
	})
}

func TestDispatcher_ExecutionControl(t *testing.T) {
	t.Parallel()

	t.Run("No Function Calls", func(t *testing.T) {
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, nil, nil)
		content := &llm.Content{Parts: []*llm.Part{}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp != nil {
			t.Errorf("expected nil response, got %v", resp)
		}
	})

	t.Run("Context Cancellation During Execution", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"tool": {Delay: 100 * time.Millisecond, Result: tools.ToolResult{Text: "ok"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "tool"}},
		}}

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := exec.Execute(ctx, content, 0, 10)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	})
}

func TestDispatcher_SuggestTool(t *testing.T) {
	t.Parallel()
	validTools := []string{"list_files", "read_file", "write_file", "git_status", "ls", "patch"}

	tests := []struct {
		hallucinated string
		expected     string
	}{
		{"list_file", "list_files"},
		{"LIST_FILE", "list_files"},
		{"read_fil", "read_file"},
		{"rit_file", "write_file"},
		{"git_stat", "git_status"},
		{"lx", "ls"},
		{"something_else", ""},
		{"get_all_outputs", ""},

		// Hits the Length <= 3 boundary (Max distance 1)
		{"lsa", "ls"}, // length 3, distance 1 -> matches "ls"

		// Hits the 3 < Length <= 6 boundary (Max distance 2)
		{"patXX", "patch"}, // length 5, distance 2 -> matches "patch"
		{"patXXX", ""},     // length 6, distance 3 -> exceeds max distance 2, should return ""

		// Hits the Length > 6 boundary (Max distance 3)
		{"list_fXXXs", "list_files"}, // length 10, distance 3 -> matches "list_files"
		{"list_XXXXs", ""},           // length 10, distance 4 -> exceeds max distance 3, should return ""
	}

	for _, tt := range tests {
		t.Run(tt.hallucinated, func(t *testing.T) {
			t.Parallel()
			got := executor.SuggestTool(tt.hallucinated, validTools)
			if got != tt.expected {
				t.Errorf("suggestTool(%q) = %q, want %q", tt.hallucinated, got, tt.expected)
			}
		})
	}
}

func TestLevenshteinDistance_UTF8(t *testing.T) {
	t.Parallel()
	tests := []struct {
		s1, s2 string
		want   int
	}{
		{"gopher", "go", 4},
		{"😊", "😊", 0},
		{"😊", "😂", 1},
		{"café", "cafe", 1},
		{"日本語", "日本", 1},
	}

	for _, tt := range tests {
		got := stringsutil.LevenshteinDistance(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
		}
	}
}

func TestDispatcher_AssembleResponse_Binary(t *testing.T) {
	t.Parallel()
	reg := testutil.NewMockToolRegistry()
	e, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, &testutil.MockEventBus{}, &ports.NoOpLogger{}, &testutil.MockLogger{})
	require.NoError(t, err)

	t.Run("Single Tool with Binary", func(t *testing.T) {
		t.Parallel()
		calls := []*llm.FunctionCall{{Name: "get_image"}}
		results := []tools.ToolResult{{
			Text:       "Here is your image",
			BinaryData: []tools.BinaryData{{MIMEType: "image/png", Data: []byte("blob")}},
		}}
		content := e.AssembleResponse(calls, results)
		if len(content.Parts) != 2 {
			t.Fatalf("Got %d parts, want 2", len(content.Parts))
		}
		assertFunctionResponse(t, content.Parts[0], "Here is your image")
		assertInlineData(t, content.Parts[1], "image/png", []byte("blob"))
	})

	t.Run("Multiple Binary Parts", func(t *testing.T) {
		t.Parallel()
		calls := []*llm.FunctionCall{{Name: "get_files"}}
		results := []tools.ToolResult{{
			BinaryData: []tools.BinaryData{
				{MIMEType: "application/pdf", Data: []byte{1, 2, 3}},
				{MIMEType: "text/plain", Data: []byte{4, 5, 6}},
			},
		}}
		content := e.AssembleResponse(calls, results)
		if len(content.Parts) != 3 {
			t.Fatalf("Got %d parts, want 3", len(content.Parts))
		}
		assertInlineData(t, content.Parts[1], "application/pdf", []byte{1, 2, 3})
		assertInlineData(t, content.Parts[2], "text/plain", []byte{4, 5, 6})
	})

	t.Run("Multi-blob Interleaving and Ordering", func(t *testing.T) {
		t.Parallel()
		calls := []*llm.FunctionCall{{Name: "camera_snapshot"}}
		results := []tools.ToolResult{{
			Text: "Captured 2 photos",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/jpeg", Data: []byte("photo1")},
				{MIMEType: "image/jpeg", Data: []byte("photo2")},
			},
		}}
		content := e.AssembleResponse(calls, results)
		assertFunctionResponse(t, content.Parts[0], "Captured 2 photos")
		assertInlineData(t, content.Parts[1], "image/jpeg", []byte("photo1"))
		assertInlineData(t, content.Parts[2], "image/jpeg", []byte("photo2"))
	})

	t.Run("Binary Data with No Text", func(t *testing.T) {
		t.Parallel()
		calls := []*llm.FunctionCall{{Name: "only_binary"}}
		results := []tools.ToolResult{{
			Text: "",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/png", Data: []byte("data")},
			},
		}}
		content := e.AssembleResponse(calls, results)
		if len(content.Parts) != 2 {
			t.Fatalf("Got %d parts, want 2", len(content.Parts))
		}
		assertFunctionResponse(t, content.Parts[0], "")
		assertInlineData(t, content.Parts[1], "image/png", []byte("data"))
	})
}

func TestDispatcher_EventPublishing(t *testing.T) {
	t.Parallel()
	reg := testutil.NewMockToolRegistry()
	err := reg.Register(&tools.ToolDeclaration{Name: "test_tool"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "success"}, nil
	})
	require.NoError(t, err)

	bus := &testutil.MockEventBus{}
	exec, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, bus, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	_, err = exec.Execute(context.Background(), content, 0, 5)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !bus.AssertEventPublished(reflect.TypeOf(events.ToolCallEvent{})) {
		t.Error("ToolCallEvent not published")
	}

	if !bus.AssertEventPublished(reflect.TypeOf(events.ToolResultEvent{})) {
		t.Error("ToolResultEvent not published")
	}
}

func TestDispatcher_Strategies(t *testing.T) {
	t.Parallel()
	reg := testutil.NewMockToolRegistry()
	e, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, &testutil.MockEventBus{}, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)

	calls := []*llm.FunctionCall{{Name: "test"}}
	results := []tools.ToolResult{{Text: "res"}}
	content := e.AssembleResponse(calls, results)
	if len(content.Parts) == 0 {
		t.Error("markdownStrategy produced no parts")
	}
}

func TestDispatcher_InternalPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Serial executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &testutil.PanicRegistry{PanicOnExec: true, Serial: true}
		bus := &testutil.MockEventBus{}
		exec, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, bus, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)})
		require.NoError(t, err)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err == nil || !errors.Is(err, llm.ErrTerminal) {
			t.Fatalf("expected terminal error, got: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool \"any\" encountered an internal fatal error (panic) and was terminated.")
		verifyToolEventError(t, bus, llm.ErrTerminal)
	})

	t.Run("Parallel executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &testutil.PanicRegistry{PanicOnExec: true, Serial: false}
		bus := &testutil.MockEventBus{}
		exec, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, bus, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)})
		require.NoError(t, err)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err == nil || !errors.Is(err, llm.ErrTerminal) {
			t.Fatalf("expected terminal error, got: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool \"any\" encountered an internal fatal error (panic) and was terminated.")
		verifyToolEventError(t, bus, llm.ErrTerminal)
	})
}

func assertFunctionResponse(t *testing.T, part *llm.Part, expectedResult interface{}) {
	t.Helper()
	if part.FunctionResponse == nil {
		t.Fatal("Expected FunctionResponse part")
	}
	res := part.FunctionResponse.Response["result"]
	if res != expectedResult {
		t.Errorf("Expected result %v, got %v", expectedResult, res)
	}
}

func assertInlineData(t *testing.T, part *llm.Part, expectedMime string, expectedData []byte) {
	t.Helper()
	if part.InlineData == nil {
		t.Fatal("Expected InlineData part")
	}
	if part.InlineData.MIMEType != expectedMime {
		t.Errorf("Expected MIME %q, got %q", expectedMime, part.InlineData.MIMEType)
	}
	if string(part.InlineData.Data) != string(expectedData) {
		t.Errorf("Expected data %q, got %q", expectedData, part.InlineData.Data)
	}
}

func TestDispatcher_SecurityAndConsentRejections(t *testing.T) {
	t.Parallel()

	t.Run("User Declined Return Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"decline_tool": {Err: tools.ErrUserDeclined},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "decline_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "The user explicitly denied this action. Do not attempt this exact action again. Ask the user for clarification or propose an alternative approach.", nil)
	})

	t.Run("Security Policy Blocked Return Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"security_tool": {Err: tools.ErrSecurityPolicy},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "security_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "Action blocked by the system sandbox security policy", tools.ErrSecurityPolicy)
	})

	t.Run("User Declined Result Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"decline_result_tool": {Result: tools.ToolResult{Error: tools.ErrUserDeclined}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "decline_result_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "The user explicitly denied this action. Do not attempt this exact action again. Ask the user for clarification or propose an alternative approach.", nil)
	})

	t.Run("Security Policy Blocked Result Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"security_result_tool": {Result: tools.ToolResult{Error: tools.ErrSecurityPolicy}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "security_result_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "Action blocked by the system sandbox security policy", tools.ErrSecurityPolicy)
	})
}

func TestDispatcher_LongRunningTimeout(t *testing.T) {
	t.Parallel()

	t.Run("Long Running Tool - Timeout Exceeded", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]testutil.ToolBehavior{
			"very_long_tool": {Delay: 500 * time.Millisecond, Result: tools.ToolResult{Text: "too late"}, Long: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil, executor.WithLongRunningTimeout(50*time.Millisecond))

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "very_long_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool execution timed out after 50ms")
	})
}

func TestDispatcher_ZombieTool(t *testing.T) {
	t.Parallel()

	reg := testutil.NewMockToolRegistry()
	zombieProceed := make(chan struct{})
	err := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: "stubborn_tool"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		<-zombieProceed // Ignores context!
		return tools.ToolResult{Text: "finally finished"}, nil
	}, tools.ToolOptions{LongRunning: true})
	require.NoError(t, err)

	exec, err := executor.NewPipelineDispatcher(reg, &testutil.MockSecurityManager{AllowAll: true}, &testutil.MockEventBus{}, &ports.NoOpLogger{}, &testutil.MockLogger{CriticalLogs: make(chan string, 10)}, executor.WithLongRunningTimeout(50*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() {
		close(zombieProceed)
	})

	content := &llm.Content{Parts: []*llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: "stubborn_tool"}},
	}}

	resp, err := exec.Execute(context.Background(), content, 0, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	verifyErrorResponse(t, resp, "Tool execution timed out after 50ms")

	// The stubborn tool is still running in the background...
	// We can't easily wait for it without some signaling mechanism in the tool,
	// but the fact that Execute returned proves that the agent is not hanging.
}
