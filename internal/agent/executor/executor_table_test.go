// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/gosharplite/tell-me-go/internal/pkg/concurrency"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"
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

func setupTestRegistry(t *testing.T, toolsMap map[string]toolBehavior) (tools.Registry, map[string]*toolBehavior) {
	t.Helper()
	reg := registry.New()
	behaviors := make(map[string]*toolBehavior)
	for name, behavior := range toolsMap {
		b := behavior
		behaviors[name] = &b
		opts := registry.ToolOptions{
			Serial:      b.serial,
			LongRunning: b.long,
		}
		if err := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: name}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			if b.observe != nil {
				b.observe()
			}
			if b.panic != nil {
				panic(b.panic)
			}
			if b.delay > 0 {
				timer := time.NewTimer(b.delay)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				case <-timer.C:
				}
			}
			return b.result, b.err
		}, opts); err != nil {
			t.Fatalf("failed to register tool %s: %v", name, err)
		}
	}
	return reg, behaviors
}

func setupMockSecurityManager(allowedTools []string) *mockSecurityManager {
	if allowedTools != nil {
		sm := &mockSecurityManager{
			allowedCommands: make(map[string]bool),
		}
		for _, tool := range allowedTools {
			sm.allowedCommands[tool] = true
		}
		return sm
	}
	return &mockSecurityManager{allowAll: true}
}

func setupTestExecutor(t *testing.T, toolsMap map[string]toolBehavior, allowedTools []string, opts ...executorOption) (*Orchestrator, *inframock.TestEventBus, map[string]*toolBehavior) {
	reg, behaviors := setupTestRegistry(t, toolsMap)
	sm := setupMockSecurityManager(allowedTools)

	bus := &inframock.TestEventBus{}
	exec, err := BuildOrchestrator(reg, sm, bus, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)}, opts...)
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

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
			t.Errorf("part %d: unexpected result: %s, want %s", i, res, expected)
		}
	}
}

func assertExecutionError(t *testing.T, resp *llm.Content, err error, bus *inframock.TestEventBus, expectedMsg string, expectedErr error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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

func verifyToolEventError(t *testing.T, bus *inframock.TestEventBus, expectedErr error) {
	t.Helper()
	evs := bus.FilterEvents(reflect.TypeOf(events.ToolResultEvent{}))
	if len(evs) == 0 {
		t.Fatalf("expected ToolResultEvent to be published")
	}
	if !errors.Is(evs[0].(events.ToolResultEvent).Result.Error, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, evs[0].(events.ToolResultEvent).Result.Error)
	}
}

func TestOrchestrator_Success(t *testing.T) {
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
		assertExecutionSuccess(t, resp, err, "res1", "res2")
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
		assertExecutionSuccess(t, resp, err, "serial_res", "res2")
	})
}

func TestOrchestrator_Errors(t *testing.T) {
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
		assertExecutionError(t, resp, err, bus, "Tool \"missing\" is not defined", llm.ErrTerminal)
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
		assertExecutionError(t, resp, err, nil, "Did you mean \"list_files\"?", nil)
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
		assertExecutionError(t, resp, err, bus, "Security policy: command \"forbidden\" is not allowed", llm.ErrTerminal)
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
		assertExecutionError(t, resp, err, bus, "tool execution failed: fail_tool: tool failed", llm.ErrTerminal)
	})
}

func TestOrchestrator_SafetyLimits(t *testing.T) {
	t.Parallel()

	t.Run("Tool Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"slow": {delay: 500 * time.Millisecond, result: tools.ToolResult{Text: "too late"}},
		}
		exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(0, 50*time.Millisecond)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "slow"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, bus, "Tool execution timed out", llm.ErrTransient)
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

	t.Run("Long Running Tool - Extended Timeout", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"long_tool": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "finally finished"}, long: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(0, 50*time.Millisecond)

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

func TestOrchestrator_PanicRecovery(t *testing.T) {
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
		assertExecutionError(t, resp, err, bus, "Tool \"panic_tool\" encountered an internal fatal error (panic) and was terminated.", llm.ErrTerminal)
	})

	t.Run("Serial Panic", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"serial_panic": {panic: "serial kaboom", serial: true},
			"next_serial":  {result: tools.ToolResult{Text: "should skip"}, serial: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "serial_panic"}},
			{FunctionCall: &llm.FunctionCall{Name: "next_serial"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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

func TestOrchestrator_Concurrency(t *testing.T) {
	t.Parallel()

	t.Run("Concurrency Limit", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"t1": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "r1"}},
			"t2": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "r2"}},
			"t3": {delay: 100 * time.Millisecond, result: tools.ToolResult{Text: "r3"}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		exec.SetConcurrency(1, 0)

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
		assertExecutionSuccess(t, resp, err, "pr1", "pr2", "sr1")
	})
}

func TestOrchestrator_ExecutionControl(t *testing.T) {
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

func TestOrchestrator_ConcurrencyLimit_Strict(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	var activeCount, maxActive int32
	blockCh := make(chan struct{})
	startedCh := make(chan struct{}, 5)

	toolFunc := createTestToolFunc(&activeCount, &maxActive, startedCh, blockCh)

	for i := 0; i < 5; i++ {
		require.NoError(t, reg.Register(&tools.ToolDeclaration{Name: fmt.Sprintf("tool%d", i)}, toolFunc))
	}

	exec, err := BuildOrchestrator(reg, &mockSecurityManager{allowAll: true}, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	exec.SetConcurrency(2, 0)
	t.Cleanup(exec.Shutdown)

	content := createTestToolContent(5)

	g := executeConcurrentWorkers(t, exec, content, startedCh, 2)
	assertConcurrencyLimit(t, &activeCount, &maxActive, 2, g, blockCh)
}

func createTestToolFunc(activeCount, maxActive *int32, startedCh chan struct{}, blockCh <-chan struct{}) tools.ToolFunc {
	return func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		current := atomic.AddInt32(activeCount, 1)
		for {
			old := atomic.LoadInt32(maxActive)
			if current <= old || atomic.CompareAndSwapInt32(maxActive, old, current) {
				break
			}
		}
		defer atomic.AddInt32(activeCount, -1)

		select {
		case startedCh <- struct{}{}:
		default:
		}

		select {
		case <-blockCh:
		case <-ctx.Done():
		}
		return tools.ToolResult{Text: "ok"}, nil
	}
}

func createTestToolContent(count int) *llm.Content {
	content := &llm.Content{}
	for i := 0; i < count; i++ {
		content.Parts = append(content.Parts, &llm.Part{
			FunctionCall: &llm.FunctionCall{Name: fmt.Sprintf("tool%d", i)},
		})
	}
	return content
}

func executeConcurrentWorkers(t *testing.T, exec *Orchestrator, content *llm.Content, startedCh <-chan struct{}, waitCount int) *errgroup.Group {
	t.Helper()
	g, _ := errgroup.WithContext(context.Background())
	g.Go(func() error {
		_, err := exec.Execute(context.Background(), content, 0, 10)
		return err
	})

	for i := 0; i < waitCount; i++ {
		timer := time.NewTimer(ciSafeTimeout)
		select {
		case <-startedCh:
			timer.Stop()
		case <-timer.C:
			t.Fatal("timeout waiting for tools to start")
		}
	}
	return g
}

func assertConcurrencyLimit(t *testing.T, activeCount, maxActive *int32, expected int32, g *errgroup.Group, blockCh chan struct{}) {
	t.Helper()
	if current := atomic.LoadInt32(activeCount); current != expected {
		t.Errorf("expected %d active tools, got %d", expected, current)
	}

	close(blockCh)
	if err := g.Wait(); err != nil {
		t.Fatalf("execution failed: %v", err)
	}

	if max := atomic.LoadInt32(maxActive); max > expected {
		t.Errorf("expected max %d concurrent tools, got %d", expected, max)
	}
}

func TestOrchestrator_SuggestTool(t *testing.T) {
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
			got := suggestTool(tt.hallucinated, validTools)
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

func TestWorkerPool_SubmitFailure(t *testing.T) {
	t.Parallel()
	p := concurrency.NewWorkerPool(1)
	p.Shutdown()

	err := p.Submit(func(ctx context.Context) {})
	if err == nil {
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
		t.Parallel()
		exec, _, _ := setupTestExecutor(t, nil, nil)
		collector := exec.newResultCollector(calls, nil)
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
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		exec, _, _ := setupTestExecutor(t, nil, nil)
		collector := exec.newResultCollector(calls, nil)
		cancel()

		_, err := collector.Wait(ctx)
		if err != context.Canceled {
			t.Errorf("Expected context.Canceled, got %v", err)
		}
	})
}

func TestOrchestrator_AssembleResponse_Binary(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	e, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{})
	require.NoError(t, err)
	t.Cleanup(e.Shutdown)

	t.Run("Single Tool with Binary", func(t *testing.T) {
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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
		t.Parallel()
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

func TestOrchestrator_EventPublishing(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	err := reg.Register(&tools.ToolDeclaration{Name: "test_tool"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "success"}, nil
	})
	require.NoError(t, err)

	bus := &inframock.TestEventBus{}
	exec, err := BuildOrchestrator(reg, nil, bus, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

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

func TestOrchestrator_Strategies(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	e, err := BuildOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
	require.NoError(t, err)
	t.Cleanup(e.Shutdown)

	calls := []*llm.FunctionCall{{Name: "test"}}
	results := []tools.ToolResult{{Text: "res"}}
	content := e.assembleResponse(calls, results)
	if len(content.Parts) == 0 {
		t.Error("markdownStrategy produced no parts")
	}
}

func TestOrchestrator_InternalPanicRecovery(t *testing.T) {
	t.Parallel()

	t.Run("Serial executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &panicRegistry{panicOnExec: true, serial: true}
		bus := &inframock.TestEventBus{}
		exec, err := BuildOrchestrator(reg, nil, bus, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
		require.NoError(t, err)
		t.Cleanup(exec.Shutdown)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		verifyErrorResponse(t, resp, "Tool \"any\" encountered an internal fatal error (panic) and was terminated.")
		verifyToolEventError(t, bus, llm.ErrTerminal)
	})

	t.Run("Parallel executeTool Panic", func(t *testing.T) {
		t.Parallel()
		reg := &panicRegistry{panicOnExec: true, serial: false}
		bus := &inframock.TestEventBus{}
		exec, err := BuildOrchestrator(reg, nil, bus, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)})
		require.NoError(t, err)
		t.Cleanup(exec.Shutdown)

		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "any"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
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

func TestOrchestrator_SecurityAndConsentRejections(t *testing.T) {
	t.Parallel()

	t.Run("User Declined Return Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"decline_tool": {err: tools.ErrUserDeclined},
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
		toolsMap := map[string]toolBehavior{
			"security_tool": {err: tools.ErrSecurityPolicy},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "security_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "Action blocked by the system sandbox security policy. You are not authorized to perform this operation.", nil)
	})

	t.Run("User Declined Result Error", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"decline_result_tool": {result: tools.ToolResult{Error: tools.ErrUserDeclined}},
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
		toolsMap := map[string]toolBehavior{
			"security_result_tool": {result: tools.ToolResult{Error: tools.ErrSecurityPolicy}},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil)
		content := &llm.Content{Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "security_result_tool"}},
		}}

		resp, err := exec.Execute(context.Background(), content, 0, 10)
		assertExecutionError(t, resp, err, nil, "Action blocked by the system sandbox security policy. You are not authorized to perform this operation.", nil)
	})
}

func TestOrchestrator_CircuitBreaker(t *testing.T) {
	t.Parallel()
	var attempts int32
	toolsMap := map[string]toolBehavior{
		"flakey_tool": {
			observe: func() { atomic.AddInt32(&attempts, 1) },
			err:     errors.New("flakey error"),
		},
	}
	exec, bus, _ := setupTestExecutor(t, toolsMap, nil)
	exec.SetConcurrency(1, 0) // serial to ensure deterministic failure counting

	content := &llm.Content{Parts: []*llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: "flakey_tool"}},
	}}

	// 1st failure
	_, _ = exec.Execute(context.Background(), content, 0, 10)
	// 2nd failure
	_, _ = exec.Execute(context.Background(), content, 0, 10)
	// 3rd failure
	_, _ = exec.Execute(context.Background(), content, 0, 10)

	// Circuit should now be open
	resp, err := exec.Execute(context.Background(), content, 0, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	verifyErrorResponse(t, resp, "temporarily disabled due to multiple consecutive failures")

	// Ensure SystemMessageEvent was published for circuit open
	evs := bus.FilterEvents(reflect.TypeOf(events.SystemMessageEvent{}))
	foundWarn := false
	for _, ev := range evs {
		sme := ev.(events.SystemMessageEvent)
		if sme.Level == "warn" && strings.Contains(sme.Message, "temporarily disabled") {
			foundWarn = true
			break
		}
	}
	if !foundWarn {
		t.Errorf("Expected SystemMessageEvent with level 'warn' for circuit breaker")
	}

	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("Expected exactly 3 attempts, got %d", attempts)
	}
}

func TestOrchestrator_ContextCancellation_Parallel(t *testing.T) {
	t.Parallel()

	toolStarted := make(chan struct{})

	toolsMap := map[string]toolBehavior{
		"tool1": {
			observe: func() {
				select {
				case <-toolStarted: // prevent double close
				default:
					close(toolStarted)
				}
			},
			delay: 100 * time.Millisecond,
		},
		"tool2": {delay: 100 * time.Millisecond},
		"tool3": {delay: 100 * time.Millisecond},
	}
	exec, _, _ := setupTestExecutor(t, toolsMap, nil)
	// Limit concurrency so tool3 is queued and picked up after context is cancelled
	exec.SetConcurrency(1, 0)

	content := &llm.Content{Parts: []*llm.Part{
		{FunctionCall: &llm.FunctionCall{Name: "tool1"}},
		{FunctionCall: &llm.FunctionCall{Name: "tool2"}},
		{FunctionCall: &llm.FunctionCall{Name: "tool3"}},
	}}

	ctx, cancel := context.WithCancel(context.Background())

	// Deterministic synchronization
	go func() {
		<-toolStarted // Block until the scheduler actually starts the tool
		cancel()
	}()

	resp, err := exec.Execute(ctx, content, 0, 10)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	// Since the executor now synthesizes responses on cancellation to preserve history state,
	// resp should NOT be nil, and the length of its parts must equal the number of original function calls.
	if resp == nil {
		t.Errorf("Expected non-nil response on context cancellation for history preservation")
	} else if len(resp.Parts) != len(content.Parts) {
		t.Errorf("Expected %d response parts, got %d", len(content.Parts), len(resp.Parts))
	}
}

func TestOrchestrator_ContextCancellation_Direct(t *testing.T) {
	t.Parallel()
	toolsMap := map[string]toolBehavior{
		"tool1": {delay: 100 * time.Millisecond},
	}
	exec, _, _ := setupTestExecutor(t, toolsMap, nil)
	exec.SetConcurrency(1, 0)

	calls := []*llm.FunctionCall{{Name: "tool1"}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	resChan := make(chan toolExecResult, 1)

	// Create wait group matching runExecutionPlan behavior manually
	var wg sync.WaitGroup
	exec.enqueueParallelTask(ctx, 0, calls[0], resChan, &wg)

	wg.Wait()

	res := <-resChan
	if res.tr.Text != "Skipped: Context cancelled" {
		t.Errorf("Expected 'Skipped: Context cancelled', got %q", res.tr.Text)
	}
}

func TestOrchestrator_UserDeclinedBatch(t *testing.T) {
	t.Parallel()
	toolsMap := map[string]toolBehavior{
		"declined_tool": {result: tools.ToolResult{Text: "ok"}},
		"allowed_tool":  {result: tools.ToolResult{Text: "ok"}},
	}
	exec, _, _ := setupTestExecutor(t, toolsMap, nil)

	calls := []*llm.FunctionCall{
		{Name: "declined_tool"},
		{Name: "allowed_tool"},
	}

	declinedMap := map[int]bool{
		0: true, // declining the first tool
	}

	resChan := make(chan toolExecResult, 2)
	batches := exec.buildExecutionBatches(calls, declinedMap, resChan)

	// Assert that the length of batches is exactly 1.
	if len(batches) != 1 {
		t.Fatalf("expected 1 batch, got %d", len(batches))
	}

	// Assert that the tasks array inside that batch contains exactly one element: index 1 (the allowed_tool).
	// It should not contain index 0.
	if len(batches[0].tasks) != 1 {
		t.Fatalf("expected 1 task in batch, got %d", len(batches[0].tasks))
	}
	if batches[0].tasks[0] != 1 {
		t.Errorf("expected task index 1, got %d", batches[0].tasks[0])
	}

	// Read one result from resChan
	res := <-resChan

	// Assert that res.index is 0, res.name is "declined_tool", and res.tr.Error is tools.ErrUserDeclined.
	if res.index != 0 {
		t.Errorf("expected result index 0, got %d", res.index)
	}
	if res.name != "declined_tool" {
		t.Errorf("expected result name 'declined_tool', got %s", res.name)
	}
	if !errors.Is(res.tr.Error, tools.ErrUserDeclined) {
		t.Errorf("expected tools.ErrUserDeclined, got %v", res.tr.Error)
	}

	// Assert that res.tr.Text equals "User explicitly denied this action."
	expectedText := "User explicitly denied this action."
	if res.tr.Text != expectedText {
		t.Errorf("expected text %q, got %q", expectedText, res.tr.Text)
	}
}

func TestOrchestrator_LongRunningTimeout(t *testing.T) {
	t.Parallel()

	t.Run("Long Running Tool - Timeout Exceeded", func(t *testing.T) {
		t.Parallel()
		toolsMap := map[string]toolBehavior{
			"very_long_tool": {delay: 500 * time.Millisecond, result: tools.ToolResult{Text: "too late"}, long: true},
		}
		exec, _, _ := setupTestExecutor(t, toolsMap, nil, WithLongRunningTimeout(50*time.Millisecond))

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

func TestOrchestrator_ZombieTool(t *testing.T) {
	t.Parallel()

	reg := registry.New()
	zombieProceed := make(chan struct{})
	err := reg.RegisterWithOptions(&tools.ToolDeclaration{Name: "stubborn_tool"}, func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		<-zombieProceed // Ignores context!
		return tools.ToolResult{Text: "finally finished"}, nil
	}, registry.ToolOptions{LongRunning: true})
	require.NoError(t, err)

	exec, err := BuildOrchestrator(reg, &mockSecurityManager{allowAll: true}, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)}, WithLongRunningTimeout(50*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(func() {
		close(zombieProceed)
		exec.Shutdown()
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
