package executor

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestToolExecutor_ConcurrentExecution(t *testing.T) {
	reg := registry.New()
	
	// Track execution concurrency
	var mu sync.Mutex
	activeCount := 0
	maxActive := 0
	
	toolFn := func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		mu.Lock()
		activeCount++
		if activeCount > maxActive {
			maxActive = activeCount
		}
		mu.Unlock()
		
		time.Sleep(100 * time.Millisecond)
		
		mu.Lock()
		activeCount--
		mu.Unlock()
		return types.ToolResult{Text: "done"}, nil
	}
	
	reg.Register(&types.ToolDeclaration{Name: "t1"}, toolFn)
	reg.Register(&types.ToolDeclaration{Name: "t2"}, toolFn)
	reg.Register(&types.ToolDeclaration{Name: "t3"}, toolFn)
	
	bus := &events.SimpleEventBus{}
	ex := NewToolExecutor(reg, nil, bus)
	ex.SetConcurrency(5, 1*time.Second)
	
	resp := &types.Content{
		Parts: []*types.Part{
			{FunctionCall: &types.FunctionCall{Name: "t1"}},
			{FunctionCall: &types.FunctionCall{Name: "t2"}},
			{FunctionCall: &types.FunctionCall{Name: "t3"}},
		},
	}
	
	_, err := ex.Execute(context.Background(), resp, 0, 10)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	
	if maxActive < 2 {
		t.Errorf("Expected at least 2 tools to run concurrently, got maxActive=%d", maxActive)
	}
}

func TestToolExecutor_SerialExecution(t *testing.T) {
	reg := registry.New()
	
	var executionOrder []string
	var mu sync.Mutex
	
	toolFn := func(name string) func(context.Context, map[string]interface{}) (types.ToolResult, error) {
		return func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
			mu.Lock()
			executionOrder = append(executionOrder, name)
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return types.ToolResult{Text: "done"}, nil
		}
	}
	
	reg.Register(&types.ToolDeclaration{Name: "p1"}, toolFn("p1"))
	reg.RegisterWithOptions(&types.ToolDeclaration{Name: "s1"}, toolFn("s1"), registry.ToolOptions{Serial: true})
	reg.Register(&types.ToolDeclaration{Name: "p2"}, toolFn("p2"))
	
	bus := &events.SimpleEventBus{}
	ex := NewToolExecutor(reg, nil, bus)
	
	resp := &types.Content{
		Parts: []*types.Part{
			{FunctionCall: &types.FunctionCall{Name: "p1"}},
			{FunctionCall: &types.FunctionCall{Name: "s1"}},
			{FunctionCall: &types.FunctionCall{Name: "p2"}},
		},
	}
	
	_, err := ex.Execute(context.Background(), resp, 0, 10)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	
	if len(executionOrder) != 3 {
		t.Fatalf("Expected 3 tools to execute, got %d", len(executionOrder))
	}
	
	// p1 starts first. s1 waits for all previous tools (p1). p2 starts after s1 because s1 is executed synchronously in executeToolsConcurrentStream.
	if executionOrder[0] != "p1" || executionOrder[1] != "s1" || executionOrder[2] != "p2" {
		t.Errorf("Unexpected execution order: %v", executionOrder)
	}
}

func TestToolExecutor_Timeout(t *testing.T) {
	reg := registry.New()
	
	reg.Register(&types.ToolDeclaration{Name: "slow"}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		select {
		case <-ctx.Done():
			return types.ToolResult{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
			return types.ToolResult{Text: "too late"}, nil
		}
	})
	
	bus := &events.SimpleEventBus{}
	ex := NewToolExecutor(reg, nil, bus)
	ex.SetConcurrency(5, 50*time.Millisecond) // Short timeout
	
	resp := &types.Content{
		Parts: []*types.Part{
			{FunctionCall: &types.FunctionCall{Name: "slow"}},
		},
	}
	
	res, err := ex.Execute(context.Background(), resp, 0, 10)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	
	if res == nil {
		t.Fatal("Execute returned nil content")
	}
	
	foundTimeout := false
	for _, p := range res.Parts {
		if p.FunctionResponse != nil {
			resStr, ok := p.FunctionResponse.Response["result"].(string)
			if ok && (strings.Contains(resStr, "timed out") || strings.Contains(strings.ToLower(resStr), "deadline exceeded")) {
				foundTimeout = true
				break
			}
		}
	}
	
	if !foundTimeout {
		var respStr string
		if len(res.Parts) > 0 && res.Parts[0].FunctionResponse != nil {
			respStr = fmt.Sprintf("%v", res.Parts[0].FunctionResponse.Response["result"])
		}
		t.Errorf("Expected timeout error in response, got %d parts. Part 0 result: %q", len(res.Parts), respStr)
	}
}

func TestWorkerPool_Shutdown(t *testing.T) {
	pool := NewWorkerPool(2)
	
	var mu sync.Mutex
	completed := 0
	
	task := func(ctx context.Context) {
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		completed++
		mu.Unlock()
	}
	
	pool.Submit(task)
	pool.Submit(task)
	pool.Submit(task)
	
	pool.Shutdown()
	
	if completed < 2 {
		t.Errorf("Expected at least 2 tasks to complete before/during shutdown, got %d", completed)
	}
}
