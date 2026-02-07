// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenerrors"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type toolBehavior struct {
	result  tools.ToolResult
	err     error
	delay   time.Duration
	panic   interface{}
	serial  bool
	long    bool
	observe func() // Callback to signal execution
}

func setupTestExecutor(t *testing.T, toolsMap map[string]toolBehavior, allowedTools []string) (*ToolExecutor, *events.TestEventBus, map[string]*toolBehavior) {
	reg := registry.New()
	behaviors := make(map[string]*toolBehavior)
	for name, behavior := range toolsMap {
		b := behavior
		behaviors[name] = &b
		opts := registry.ToolOptions{
			Serial:      b.serial,
			LongRunning: b.long,
		}
		reg.RegisterWithOptions(&tools.ToolDeclaration{Name: name}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
			if b.observe != nil {
				b.observe()
			}
			if b.panic != nil {
				panic(b.panic)
			}
			if b.delay > 0 {
				select {
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				case <-time.After(b.delay):
				}
			}
			return b.result, b.err
		}, opts)
	}

	var sm *mockSecurityManager
	if allowedTools != nil {
		sm = &mockSecurityManager{
			allowedCommands: make(map[string]bool),
		}
		for _, tool := range allowedTools {
			sm.allowedCommands[tool] = true
		}
	} else {
		sm = &mockSecurityManager{allowAll: true}
	}

	bus := &events.TestEventBus{}
	exec := NewToolExecutor(reg, sm, bus)
	exec.SetStrategy(&MockStrategy{}) // Use simple strategy for easier verification
	t.Cleanup(exec.Shutdown)

	return exec, bus, behaviors
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

func verifyToolEventError(t *testing.T, bus *events.TestEventBus, expectedErr error) {
	t.Helper()
	evs := bus.FilterEvents(reflect.TypeOf(events.ToolResultEvent{}))
	if len(evs) == 0 {
		t.Fatalf("expected ToolResultEvent to be published")
	}
	if !errors.Is(evs[0].(events.ToolResultEvent).Result.Error, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, evs[0].(events.ToolResultEvent).Result.Error)
	}
}

func TestToolExecutor_Success(t *testing.T) {
	t.Parallel()

	t.Run("Parallel Execution", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"tool1": {result: tools.ToolResult{Text: "res1"}},
			"tool2": {result: tools.ToolResult{Text: "res2"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2, 0)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "tool1"}},
			{FunctionCall: &llm.FunctionCall{Name: "tool2"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if resp == nil || len(resp.Parts) != 2 {
			t.Fatalf("expected 2 response parts, got %v", resp)
		}
		res1 := resp.Parts[0].FunctionResponse.Response["result"].(string)
		res2 := resp.Parts[1].FunctionResponse.Response["result"].(string)
		if res1 != "res1" || res2 != "res2" {
			t.Errorf("unexpected results: %s, %s", res1, res2)
		}
	})

	t.Run("Sequential Execution", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"serial_tool": {result: tools.ToolResult{Text: "serial_res"}, serial: true},
			"tool2":       {result: tools.ToolResult{Text: "res2"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2, 0)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_tool"}},
			{FunctionCall: &llm.FunctionCall{Name: "tool2"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Parts) != 2 {
			t.Fatalf("expected 2 response parts, got %d", len(resp.Parts))
		}
	})
}

func TestToolExecutor_Errors(t *testing.T) {
	t.Parallel()

	t.Run("Tool Not Found", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"existing": {result: tools.ToolResult{Text: "ok"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "missing"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		verifyErrorResponse(t, resp, "Tool \"missing\" is not defined")
		verifyToolEventError(t, bus, agenerrors.ErrLogic)
	})

	t.Run("Tool Suggestion", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"list_files": {result: tools.ToolResult{Text: "ok"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "list_file"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Did you mean \"list_files\"?")
	})

	t.Run("Security Violation", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"forbidden": {result: tools.ToolResult{Text: "ok"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, []string{"allowed"})
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "forbidden"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Security policy: command \"forbidden\" is not allowed")
		verifyToolEventError(t, bus, agenerrors.ErrLogic)
	})

	t.Run("Tool Returns Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"fail_tool": {err: errors.New("tool failed")},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "fail_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "tool execution failed: fail_tool: tool failed")
		verifyToolEventError(t, bus, agenerrors.ErrLogic)
	})
}

func TestToolExecutor_SafetyLimits(t *testing.T) {
	t.Parallel()

	t.Run("Tool Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"slow": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "too late"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(0, 10*time.Millisecond)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "slow"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool execution timed out")
		verifyToolEventError(t, bus, agenerrors.ErrTransient)
	})

	t.Run("Max Turns Reached", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"tool": {result: tools.ToolResult{Text: "ok"}},
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

	t.Run("Long Running Tool - No Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"long_tool": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "finally finished"}, long: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(0, 10*time.Millisecond)

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

func TestToolExecutor_PanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Parallel Panic", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"panic_tool": {panic: "kaboom"},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "panic_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Panic detected: kaboom")
		verifyToolEventError(t, bus, agenerrors.ErrFatal)
	})

	t.Run("Serial Panic", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"serial_panic": {panic: "serial kaboom", serial: true},
			"next_tool":    {result: tools.ToolResult{Text: "should skip"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_panic"}},
			{FunctionCall: &llm.FunctionCall{Name: "next_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp == nil || len(resp.Parts) < 2 {
			t.Fatalf("expected at least 2 parts, got %v", resp)
		}
		res0 := resp.Parts[0].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res0, "Panic detected: serial kaboom") {
			t.Errorf("expected serial panic error, got %s", res0)
		}
		res1 := resp.Parts[1].FunctionResponse.Response["result"].(string)
		if !strings.Contains(res1, "Skipped: Execution halted") {
			t.Errorf("expected skipped message, got %s", res1)
		}
	})
}

func TestToolExecutor_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("Concurrency Limit", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"t1": {delay: 50 * time.Millisecond, result: tools.ToolResult{Text: "r1"}},
			"t2": {delay: 50 * time.Millisecond, result: tools.ToolResult{Text: "r2"}},
			"t3": {delay: 50 * time.Millisecond, result: tools.ToolResult{Text: "r3"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(1, 0)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "t1"}},
			{FunctionCall: &llm.FunctionCall{Name: "t2"}},
			{FunctionCall: &llm.FunctionCall{Name: "t3"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Parts) != 3 {
			t.Fatalf("expected 3 results, got %d", len(resp.Parts))
		}
	})

	t.Run("Mixed Path - Parallel then Serial", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"p1": {result: tools.ToolResult{Text: "pr1"}},
			"p2": {result: tools.ToolResult{Text: "pr2"}},
			"s1": {result: tools.ToolResult{Text: "sr1"}, serial: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(2, 0)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "p1"}},
			{FunctionCall: &llm.FunctionCall{Name: "p2"}},
			{FunctionCall: &llm.FunctionCall{Name: "s1"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Parts) != 3 {
			t.Fatalf("expected 3 results, got %d", len(resp.Parts))
		}
	})
}

func TestToolExecutor_ExecutionControl(t *testing.T) {
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
		toolsMap := map[string]toolBehavior{
			"tool": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "ok"}},
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

func TestToolExecutor_ConcurrencyLimit_Strict(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	var activeCount int32
	var maxActive int32

	toolFunc := func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		current := atomic.AddInt32(&activeCount, 1)
		for {
			old := atomic.LoadInt32(&maxActive)
			if current <= old || atomic.CompareAndSwapInt32(&maxActive, old, current) {
				break
			}
		}
		defer atomic.AddInt32(&activeCount, -1)
		time.Sleep(20 * time.Millisecond)
		return tools.ToolResult{Text: "ok"}, nil
	}

	for i := 0; i < 5; i++ {
		reg.Register(&tools.ToolDeclaration{Name: fmt.Sprintf("tool%d", i)}, toolFunc)
	}

	exec := NewToolExecutor(reg, &mockSecurityManager{allowAll: true}, nil)
	exec.SetConcurrency(2, 0)
	t.Cleanup(exec.Shutdown)

	content := &llm.Content{}
	for i := 0; i < 5; i++ {
		content.Parts = append(content.Parts, &llm.Part{FunctionCall: &llm.FunctionCall{Name: fmt.Sprintf("tool%d", i)}})
	}

	_, err := exec.Execute(context.Background(), content, 0, 10)
	if err != nil {
		t.Fatal(err)
	}

	if atomic.LoadInt32(&maxActive) > 2 {
		t.Errorf("expected max 2 concurrent tools, got %d", maxActive)
	}
}

func TestToolExecutor_SuggestTool(t *testing.T) {
	exec := &ToolExecutor{}
	validTools := []string{"list_files", "read_file", "write_file", "git_status", "ls"}

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
	}

	for _, tt := range tests {
		t.Run(tt.hallucinated, func(t *testing.T) {
			got := exec.suggestTool(tt.hallucinated, validTools)
			if got != tt.expected {
				t.Errorf("suggestTool(%q) = %q, want %q", tt.hallucinated, got, tt.expected)
			}
		})
	}
}

func TestLevenshteinDistance_UTF8(t *testing.T) {
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
		got := levenshteinDistance(tt.s1, tt.s2)
		if got != tt.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, got, tt.want)
		}
	}
}

func TestWorkerPool_SubmitFailure(t *testing.T) {
	p := NewWorkerPool(1)
	p.Shutdown()

	success := p.Submit(func(ctx context.Context) {})
	if success {
		t.Error("Expected Submit to fail on closed pool")
	}
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

func TestToolExecutor_AssembleResponse_Binary(t *testing.T) {
	t.Parallel()
	e := &ToolExecutor{strategy: &MockStrategy{}}

	t.Run("Single Tool with Binary", func(t *testing.T) {
		calls := []*llm.FunctionCall{{Name: "get_image"}}
		results := []tools.ToolResult{{
			Text:       "Here is your image",
			BinaryData: []tools.BinaryData{{MIMEType: "image/png", Data: []byte("blob")}},
		}}
		content := e.assembleResponse(calls, results)
		if len(content.Parts) != 2 {
			t.Fatalf("Got %d parts, want 2", len(content.Parts))
		}
		assertFunctionResponse(t, content.Parts[0], "Here is your image")
		assertInlineData(t, content.Parts[1], "image/png", []byte("blob"))
	})

	t.Run("Multiple Binary Parts", func(t *testing.T) {
		calls := []*llm.FunctionCall{{Name: "get_files"}}
		results := []tools.ToolResult{{
			BinaryData: []tools.BinaryData{
				{MIMEType: "application/pdf", Data: []byte{1, 2, 3}},
				{MIMEType: "text/plain", Data: []byte{4, 5, 6}},
			},
		}}
		content := e.assembleResponse(calls, results)
		if len(content.Parts) != 3 {
			t.Fatalf("Got %d parts, want 3", len(content.Parts))
		}
		assertInlineData(t, content.Parts[1], "application/pdf", []byte{1, 2, 3})
		assertInlineData(t, content.Parts[2], "text/plain", []byte{4, 5, 6})
	})

	t.Run("Multi-blob Interleaving and Ordering", func(t *testing.T) {
		calls := []*llm.FunctionCall{{Name: "camera_snapshot"}}
		results := []tools.ToolResult{{
			Text: "Captured 2 photos",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/jpeg", Data: []byte("photo1")},
				{MIMEType: "image/jpeg", Data: []byte("photo2")},
			},
		}}
		content := e.assembleResponse(calls, results)
		assertFunctionResponse(t, content.Parts[0], "Captured 2 photos")
		assertInlineData(t, content.Parts[1], "image/jpeg", []byte("photo1"))
		assertInlineData(t, content.Parts[2], "image/jpeg", []byte("photo2"))
	})

	t.Run("Binary Data with No Text", func(t *testing.T) {
		calls := []*llm.FunctionCall{{Name: "only_binary"}}
		results := []tools.ToolResult{{
			Text: "",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/png", Data: []byte("data")},
			},
		}}
		content := e.assembleResponse(calls, results)
		if len(content.Parts) != 2 {
			t.Fatalf("Got %d parts, want 2", len(content.Parts))
		}
		assertFunctionResponse(t, content.Parts[0], "")
		assertInlineData(t, content.Parts[1], "image/png", []byte("data"))
	})
}

func TestToolExecutor_EventPublishing(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{Name: "test_tool"}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "success"}, nil
	})

	bus := &events.TestEventBus{}
	exec := NewToolExecutor(reg, nil, bus)
	t.Cleanup(exec.Shutdown)

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	_, err := exec.Execute(context.Background(), content, 0, 5)
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

func TestToolExecutor_Strategies(t *testing.T) {
	reg := registry.New()
	e := NewToolExecutor(reg, nil, nil)
	t.Cleanup(e.Shutdown)
	e.SetStrategy(&MarkdownStrategy{})
	e.SetStrategy(&JSONStrategy{})

	calls := []*llm.FunctionCall{{Name: "test"}}
	results := []tools.ToolResult{{Text: "res"}}
	content := e.assembleResponse(calls, results)
	if len(content.Parts) == 0 {
		t.Error("JSONStrategy produced no parts")
	}
}

type mockSecurityManager struct {
	domain_security.ISecurityManager
	allowedCommands map[string]bool
	allowAll        bool
}

func (m *mockSecurityManager) IsCommandAllowed(command string) bool {
	if m.allowAll {
		return true
	}
	return m.allowedCommands[command]
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

type panicRegistry struct {
	tools.IToolRegistry
	panicOnGet bool
	serial     bool
}

func (r *panicRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if r.panicOnGet {
		panic("registry GetDeclarations panic")
	}
	return []*tools.ToolDeclaration{{Name: "panic_tool"}}
}

func (r *panicRegistry) IsSerial(name string) bool {
	return r.serial
}

func (r *panicRegistry) IsLongRunning(name string) bool {
	return false
}

func (r *panicRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func TestToolExecutor_InternalPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Serial executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &panicRegistry{panicOnGet: true, serial: true}
		bus := &events.TestEventBus{}
		exec := NewToolExecutor(reg, nil, bus)
		defer exec.Shutdown()
		exec.SetStrategy(&MockStrategy{})

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Panic detected: registry GetDeclarations panic")
		verifyToolEventError(t, bus, agenerrors.ErrFatal)
	})

	t.Run("Parallel executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &panicRegistry{panicOnGet: true, serial: false}
		bus := &events.TestEventBus{}
		exec := NewToolExecutor(reg, nil, bus)
		defer exec.Shutdown()
		exec.SetStrategy(&MockStrategy{})

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Panic detected: registry GetDeclarations panic")
		verifyToolEventError(t, bus, agenerrors.ErrFatal)
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
