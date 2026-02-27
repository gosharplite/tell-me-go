package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

type mockZombieRegistry struct {
	executeFn func(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error)
}

func (m *mockZombieRegistry) GetDeclarations() []*domaintools.ToolDeclaration {
	return []*domaintools.ToolDeclaration{{Name: "hanging_tool"}}
}
func (m *mockZombieRegistry) Register(d *domaintools.ToolDeclaration, f domaintools.ToolFunc) {}
func (m *mockZombieRegistry) RegisterWithOptions(def *domaintools.ToolDeclaration, handler domaintools.ToolFunc, opts domaintools.ToolOptions) {
}
func (m *mockZombieRegistry) Execute(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args)
	}
	return domaintools.ToolResult{Text: "ok"}, nil
}
func (m *mockZombieRegistry) IsLongRunning(name string) bool { return false }
func (m *mockZombieRegistry) IsSerial(name string) bool      { return false }

func TestToolExecutor_GoroutineLeak(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow integration test in short mode")
	}
	// A simple tool that sleeps longer than the timeout

	hangingTool := &domaintools.ToolDeclaration{
		Name:        "hanging_tool",
		Description: "I hang forever",
	}

	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error) {
			<-ctx.Done()                      // Block until context canceled
			time.Sleep(10 * time.Millisecond) // Simulate some cleanup that ignores ctx
			return domaintools.ToolResult{Text: "done"}, nil
		},
	}

	exec := NewToolExecutor(reg, nil, nil)
	t.Cleanup(exec.Shutdown)
	// mock the toolTimeout since NewToolExecutor sets it to default
	exec.toolTimeout = 5 * time.Millisecond

	ctx := context.Background()

	result, err := exec.runWithTimeout(ctx, hangingTool, nil)
	if err != nil {
		t.Fatalf("expected transient err embedded in result, got %v", err)
	}

	if result.Error == nil {
		t.Fatalf("expected error inside result but got nil")
	}

	if !strings.Contains(result.Error.Error(), "timed out") && !strings.Contains(result.Error.Error(), "canceled") && !strings.Contains(result.Error.Error(), llm.ErrTransient.Error()) {
		t.Fatalf("expected timeout or transient error, got %v", result.Error)
	}
}

type MockLogger struct {
	CriticalLogs chan string
}

func (m *MockLogger) LogCritical(msg string) {
	m.CriticalLogs <- msg
}

func (m *MockLogger) RecordLateCompletion(name string, d time.Duration) {
	// Not used in this test
}

func TestToolExecutor_ZombieTool_LogCritical(t *testing.T) {
	t.Parallel()

	mockLog := &MockLogger{CriticalLogs: make(chan string, 1)}
	finishCh := make(chan struct{})
	defer close(finishCh)

	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}) (domaintools.ToolResult, error) {
			// Simulate a tool that blocks indefinitely even if context is canceled
			// but we use a channel so we can clean it up for goleak
			<-finishCh
			return domaintools.ToolResult{}, nil
		},
	}

	// Use very short zombie timeout
	exec := NewToolExecutor(reg, nil, nil, 
		WithZombieTimeout(1*time.Millisecond),
		WithLogger(mockLog),
	)
	t.Cleanup(exec.Shutdown)
	exec.toolTimeout = 1 * time.Millisecond

	hangingTool := &domaintools.ToolDeclaration{Name: "hanging_tool"}

	// Should timeout
	_, _ = exec.runWithTimeout(context.Background(), hangingTool, nil)

	// Blocks exactly until the log occurs, or times out cleanly
	select {
	case msg := <-mockLog.CriticalLogs:
		assert.Contains(t, msg, "CRITICAL: Tool goroutine permanently leaked")
		assert.Contains(t, msg, "hanging_tool")
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for critical log")
	}
}
