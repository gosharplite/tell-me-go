package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockZombieRegistry struct {
	executeFn         func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)
	getDeclarationsFn func() []*tools.ToolDeclaration
	isSerialFn        func(name string) bool
	isLongRunningFn   func(name string) bool
}

func (m *mockZombieRegistry) GetDeclarations() []*tools.ToolDeclaration {
	if m.getDeclarationsFn != nil {
		return m.getDeclarationsFn()
	}
	return []*tools.ToolDeclaration{{Name: "hanging_tool"}}
}
func (m *mockZombieRegistry) Register(d *tools.ToolDeclaration, f tools.ToolFunc) error {
	return nil
}
func (m *mockZombieRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return nil
}
func (m *mockZombieRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args)
	}
	return tools.ToolResult{Text: "ok"}, nil
}
func (m *mockZombieRegistry) IsLongRunning(name string) bool {
	if m.isLongRunningFn != nil {
		return m.isLongRunningFn(name)
	}
	return false
}
func (m *mockZombieRegistry) IsSerial(name string) bool {
	if m.isSerialFn != nil {
		return m.isSerialFn(name)
	}
	return false
}

func TestOrchestrator_GoroutineLeak(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	// A simple tool that sleeps longer than the timeout

	hangingTool := &tools.ToolDeclaration{
		Name:        "hanging_tool",
		Description: "I hang forever",
	}

	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			<-ctx.Done() // Block until context canceled
			return tools.ToolResult{Text: "done"}, nil
		},
	}

	exec, err := NewOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{CriticalLogs: make(chan string, 10)}, withToolTimeout(200*time.Millisecond))
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()

	doneCh := make(chan struct{})
	var result tools.ToolResult
	var timeoutErr error

	go func() {
		defer close(doneCh)
		fc := &llm.FunctionCall{Name: hangingTool.Name}
		tool, _ := exec.resolver.Resolve(fc)
		result, timeoutErr = exec.runtime.Execute(ctx, tool, fc)
	}()

	select {
	case <-doneCh:
		if timeoutErr != nil {
			t.Fatalf("expected transient err embedded in result, got %v", timeoutErr)
		}

		if result.Error == nil {
			t.Fatalf("expected error inside result but got nil")
		}

		if !strings.Contains(result.Error.Error(), "timed out") && !strings.Contains(result.Error.Error(), "canceled") && !strings.Contains(result.Error.Error(), llm.ErrTransient.Error()) {
			t.Fatalf("expected timeout or transient error, got %v", result.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Test deadlocked on runWithTimeout")
	}
}

func TestOrchestrator_ZombieTool_LogCritical(t *testing.T) {
	t.Parallel()

	mockLog := &MockLogger{CriticalLogs: make(chan string, 1)}
	finishCh := make(chan struct{})
	defer close(finishCh)

	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			// Simulate a tool that blocks indefinitely even if context is canceled
			// but we use a channel so we can clean it up for goleak
			<-finishCh
			return tools.ToolResult{}, nil
		},
	}

	// Use short zombie timeout, but generous enough for -race
	exec, err := NewOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, mockLog,
		withZombieTimeout(200*time.Millisecond),
		withToolTimeout(200*time.Millisecond),
	)
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	hangingTool := &tools.ToolDeclaration{Name: "hanging_tool"}

	// Should timeout
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		fc := &llm.FunctionCall{Name: hangingTool.Name}
		tool, _ := exec.resolver.Resolve(fc)
		_, _ = exec.runtime.Execute(context.Background(), tool, fc)
	}()

	select {
	case <-doneCh:
	case <-time.After(2 * time.Second):
		t.Fatal("Test deadlocked on runWithTimeout")
	}

	// Blocks exactly until the log occurs, or times out cleanly
	timer := time.NewTimer(ciSafeTimeout)
	defer timer.Stop()

	select {
	case msg := <-mockLog.CriticalLogs:
		assert.Contains(t, msg, "CRITICAL: Tool goroutine permanently leaked")
		assert.Contains(t, msg, "hanging_tool")
	case <-timer.C:
		t.Fatal("Timeout waiting for critical log")
	}
}
