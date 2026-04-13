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
	executeFn         func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error)
	getDeclarationsFn func() []*tools.ToolDeclaration
	isSerialFn        func(name string) bool
	isLongRunningFn   func(name string) bool
	livenessThreshold time.Duration
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
func (m *mockZombieRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	if m.executeFn != nil {
		return m.executeFn(ctx, name, args, hb)
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

func TestDispatcher_GoroutineLeak(t *testing.T) {
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
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			<-ctx.Done() // Block until context canceled
			return tools.ToolResult{Text: "done"}, nil
		},
	}

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{CriticalLogs: make(chan string, 10)}, WithToolTimeout(200*time.Millisecond))
	require.NoError(t, err)

	ctx := context.Background()

	doneCh := make(chan struct{})
	var result tools.ToolResult
	var timeoutErr error

	go func() {
		defer close(doneCh)
		fc := &llm.FunctionCall{Name: hangingTool.Name}
		tool, _ := exec.pipeline.(*defaultToolPipeline).resolver.Resolve(fc)
		result, timeoutErr = exec.pipeline.(*defaultToolPipeline).runtime.Execute(ctx, tool, fc, nil)
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

func TestDispatcher_ZombieTool_LogCritical(t *testing.T) {
	t.Parallel()

	mockLog := &mockLogger{CriticalLogs: make(chan string, 1)}
	finishCh := make(chan struct{})
	defer close(finishCh)

	reg := &mockZombieRegistry{
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			// Simulate a tool that blocks indefinitely even if context is canceled
			// but we use a channel so we can clean it up for goleak
			<-finishCh
			return tools.ToolResult{}, nil
		},
	}

	// Use short zombie timeout, but generous enough for -race
	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, mockLog,
		withZombieTimeout(200*time.Millisecond),
		WithToolTimeout(200*time.Millisecond),
	)
	require.NoError(t, err)

	hangingTool := &tools.ToolDeclaration{Name: "hanging_tool"}

	// Should timeout
	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		fc := &llm.FunctionCall{Name: hangingTool.Name}
		tool, _ := exec.pipeline.(*defaultToolPipeline).resolver.Resolve(fc)
		_, _ = exec.pipeline.(*defaultToolPipeline).runtime.Execute(context.Background(), tool, fc, nil)
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

func (m *mockZombieRegistry) GetOptions(name string) tools.ToolOptions {
	return tools.ToolOptions{
		LongRunning:       m.IsLongRunning(name),
		Serial:            m.IsSerial(name),
		LivenessThreshold: m.livenessThreshold,
	}
}

func TestDispatcher_ZombieHeartbeatDetection(t *testing.T) {
	t.Parallel()

	mockLog := &mockLogger{CriticalLogs: make(chan string, 10)}

	// Create a tool that emits heartbeats for a while, then goes "zombie"
	reg := &mockZombieRegistry{
		livenessThreshold: 100 * time.Millisecond,
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			// Initially active: emit 2 heartbeats
			for i := 0; i < 2; i++ {
				select {
				case hb <- struct{}{}:
				case <-ctx.Done():
					return tools.ToolResult{}, ctx.Err()
				}
				time.Sleep(50 * time.Millisecond)
			}

			// Become a zombie: infinite loop without heartbeats
			// Must still check ctx.Done() to allow clean exit when dispatcher cancels it.
			for {
				select {
				case <-ctx.Done():
					return tools.ToolResult{Text: "cancelled"}, ctx.Err()
				default:
					// Simulate heavy computation or hanging I/O
					time.Sleep(10 * time.Millisecond)
				}
			}
		},
	}

	// Dispatcher with long global timeout (5s) but short liveness threshold (100ms)
	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, mockLog,
		WithToolTimeout(5*time.Second),
		WithLongRunningTimeout(5*time.Second),
	)
	require.NoError(t, err)

	fc := &llm.FunctionCall{Name: "hanging_tool"}
	tool, _ := exec.pipeline.(*defaultToolPipeline).resolver.Resolve(fc)

	start := time.Now()
	doneCh := make(chan struct{})
	var result tools.ToolResult
	go func() {
		defer close(doneCh)
		result, _ = exec.pipeline.(*defaultToolPipeline).runtime.Execute(context.Background(), tool, fc, nil)
	}()

	select {
	case <-doneCh:
		duration := time.Since(start)
		// Should have been cancelled after:
		// ~100ms (2 heartbeats * 50ms) + ~100ms (threshold) = ~200ms
		// We assert it's less than 1 second, proving it didn't wait for 5s global timeout.
		assert.Less(t, duration, 1*time.Second, "Zombie tool should be cancelled by liveness threshold (%v), not global timeout (5s)", duration)
		assert.Error(t, result.Error)
		assert.Contains(t, result.Error.Error(), "failed")
	case <-time.After(6 * time.Second):
		t.Fatal("Test timed out: Dispatcher failed to cancel the zombie tool")
	}
}

func (m *mockZombieRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *mockZombieRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *mockZombieRegistry) GetCoreDeclarations() []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockZombieRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return m.GetDeclarations()
}

func (m *mockZombieRegistry) ListAvailableToolkits() []string {
	return []string{"core"}
}
