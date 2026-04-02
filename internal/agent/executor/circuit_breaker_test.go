package executor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

func TestCircuitBreakerPipeline_StateTransitions(t *testing.T) {
	t.Parallel()
	errSimulated := errors.New("simulated tool error")
	threshold := 3
	resetTimeout := 50 * time.Millisecond

	t.Run("SuccessKeepsCircuitClosed", func(t *testing.T) {
		t.Parallel()
		mock := &MockToolPipeline{
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				return tools.ToolResult{Text: "success"}
			},
			IsSerialFunc: func(n string) bool { return false },
		}
		mockClock := clock.NewMockClock(time.Now())
		cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))

		call := &llm.FunctionCall{Name: "reliable_tool"}
		for i := 0; i < 5; i++ {
			res := cbPipeline.ExecuteTool(context.Background(), call)
			if res.Error != nil {
				t.Fatalf("expected success, got error: %v", res.Error)
			}
		}
	})

	t.Run("FailuresTriggerOpenCircuit", func(t *testing.T) {
		t.Parallel()
		mock := &MockToolPipeline{
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				return tools.ToolResult{Error: errSimulated}
			},
			IsSerialFunc: func(n string) bool { return false },
		}
		mockClock := clock.NewMockClock(time.Now())
		cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))
		call := &llm.FunctionCall{Name: "failing_tool"}

		// Hit the threshold
		for i := 0; i < threshold; i++ {
			res := cbPipeline.ExecuteTool(context.Background(), call)
			if res.Error == nil {
				t.Fatalf("expected tool error, got success on execution %d", i)
			}
		}

		// Next call should fail immediately with ErrToolCircuitOpen
		res := cbPipeline.ExecuteTool(context.Background(), call)
		if !errors.Is(res.Error, tools.ErrToolCircuitOpen) {
			t.Fatalf("expected circuit open error, got: %v", res.Error)
		}
	})

	t.Run("HalfOpenTransitionAfterTimeout", func(t *testing.T) {
		t.Parallel()
		mock := &MockToolPipeline{
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				return tools.ToolResult{Error: errSimulated}
			},
			IsSerialFunc: func(n string) bool { return false },
		}
		mockClock := clock.NewMockClock(time.Now())
		cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))
		call := &llm.FunctionCall{Name: "recovering_tool"}

		// Hit the threshold to open circuit
		for i := 0; i < threshold; i++ {
			cbPipeline.ExecuteTool(context.Background(), call)
		}

		// Verify circuit is open
		res := cbPipeline.ExecuteTool(context.Background(), call)
		if !errors.Is(res.Error, tools.ErrToolCircuitOpen) {
			t.Fatalf("expected circuit open error, got: %v", res.Error)
		}

		// Advance clock past the reset timeout
		mockClock.Advance(resetTimeout * 2)

		// Next call should transition to half-open, try the tool again, and get the mock tool error
		res = cbPipeline.ExecuteTool(context.Background(), call)
		if res.Error == nil || errors.Is(res.Error, tools.ErrToolCircuitOpen) {
			t.Fatalf("expected simulated mock error in half-open state, got: %v", res.Error)
		}
	})
}

func TestCircuitBreakerPipeline_Recovery(t *testing.T) {
	t.Parallel()
	threshold := 2
	resetTimeout := 50 * time.Millisecond

	var forceError bool

	mock := &MockToolPipeline{
		ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
			if forceError {
				return tools.ToolResult{Error: errors.New("forced failure")}
			}
			return tools.ToolResult{Text: "success"}
		},
	}

	mockClock := clock.NewMockClock(time.Now())
	cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))
	call := &llm.FunctionCall{Name: "flaky_tool"}

	// 1. Force Failures to open circuit
	forceError = true
	cbPipeline.ExecuteTool(context.Background(), call)
	cbPipeline.ExecuteTool(context.Background(), call) // Reaches threshold

	res := cbPipeline.ExecuteTool(context.Background(), call) // Should be Open
	if !errors.Is(res.Error, tools.ErrToolCircuitOpen) {
		t.Fatalf("expected circuit to be open, got: %v", res.Error)
	}

	// 2. Wait for timeout to transition to half-open
	mockClock.Advance(resetTimeout * 2)

	// 3. Recover the tool
	forceError = false
	res = cbPipeline.ExecuteTool(context.Background(), call) // Should succeed and transition to Closed
	if res.Error != nil {
		t.Fatalf("expected successful recovery, got: %v", res.Error)
	}

	// 4. Verify it's closed
	res = cbPipeline.ExecuteTool(context.Background(), call)
	if res.Error != nil {
		t.Fatalf("expected circuit to remain closed, got: %v", res.Error)
	}
}

func TestCircuitBreakerPipeline_Delegation(t *testing.T) {
	t.Parallel()
	mock := &MockToolPipeline{
		RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
			return ctx, map[int]bool{0: true}
		},
		IsSerialFunc: func(toolName string) bool {
			return toolName == "serial_tool"
		},
	}
	cbPipeline := NewCircuitBreakerPipeline(mock, 1, time.Second)

	if !cbPipeline.IsSerial("serial_tool") {
		t.Error("expected IsSerial to be delegated")
	}
	if cbPipeline.IsSerial("parallel_tool") {
		t.Error("expected IsSerial to return false for parallel_tool")
	}

	_, m := cbPipeline.RequestBatchConsent(context.Background(), nil)
	if !m[0] {
		t.Error("expected RequestBatchConsent to be delegated")
	}
}

func TestCircuitBreakerPipeline_ConcurrentTripping(t *testing.T) {
	t.Parallel()
	threshold := 5
	resetTimeout := 50 * time.Millisecond

	mock := &MockToolPipeline{
		ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
			return tools.ToolResult{Error: errors.New("simulated concurrent failure")}
		},
		IsSerialFunc: func(n string) bool { return false },
	}

	mockClock := clock.NewMockClock(time.Now())
	cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))
	call := &llm.FunctionCall{Name: "concurrent_failing_tool"}

	numGoroutines := 150
	errCh := make(chan error, numGoroutines*5)

	var isClosed bool
	var closeMu sync.Mutex
	t.Cleanup(func() {
		closeMu.Lock()
		defer closeMu.Unlock()
		if !isClosed {
			close(errCh)
			isClosed = true
		}
	})

	var startWg sync.WaitGroup
	var doneWg sync.WaitGroup
	startWg.Add(1)

	for i := 0; i < numGoroutines; i++ {
		doneWg.Add(1)
		go func() {
			defer doneWg.Done()
			startWg.Wait()
			for j := 0; j < 5; j++ {
				res := cbPipeline.ExecuteTool(context.Background(), call)
				errCh <- res.Error
			}
		}()
	}

	startWg.Done()
	doneWg.Wait()

	closeMu.Lock()
	if !isClosed {
		close(errCh)
		isClosed = true
	}
	closeMu.Unlock()

	var circuitOpenErrors int
	for err := range errCh {
		if errors.Is(err, tools.ErrToolCircuitOpen) {
			circuitOpenErrors++
		}
	}

	circuit := cbPipeline.getCircuit("concurrent_failing_tool")
	state := circuit.state.Load()

	if state != int32(StateOpen) {
		t.Errorf("expected state to be StateOpen, got %d", state)
	}

	if circuitOpenErrors == 0 {
		t.Errorf("expected some circuit open errors due to concurrency, got 0")
	}

	finalFailures := circuit.failures.Load()
	if finalFailures > int64(numGoroutines*5) {
		t.Errorf("expected failures to be <= %d, got %d", numGoroutines*5, finalFailures)
	}
}
