package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

func TestCircuitBreakerPipeline_StateTransitions(t *testing.T) {
	errSimulated := errors.New("simulated tool error")
	threshold := 3
	resetTimeout := 50 * time.Millisecond // short timeout for testing

	tests := []struct {
		name          string
		toolName      string
		executions    int // number of initial executions
		simulateError bool
		wantFinalErr  error
		waitTimeout   bool // wait for resetTimeout before next check
		afterWaitErr  error
	}{
		{
			name:          "success keeps circuit closed",
			toolName:      "reliable_tool",
			executions:    5,
			simulateError: false,
			wantFinalErr:  nil,
		},
		{
			name:          "failures trigger open circuit",
			toolName:      "failing_tool",
			executions:    threshold,
			simulateError: true,
			wantFinalErr:  tools.ErrToolCircuitOpen,
		},
		{
			name:          "half-open transition after timeout",
			toolName:      "recovering_tool",
			executions:    threshold,
			simulateError: true,
			wantFinalErr:  tools.ErrToolCircuitOpen,
			waitTimeout:   true,
			afterWaitErr:  errSimulated, // Next call still fails because simulateError is still true, but it doesn't return ErrToolCircuitOpen immediately
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &MockToolPipeline{
				ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
					if tt.simulateError {
						return tools.ToolResult{Error: errSimulated}
					}
					return tools.ToolResult{Text: "success"}
				},
				IsSerialFunc: func(n string) bool { return false },
			}

			mockClock := clock.NewMockClock(time.Now())
			cbPipeline := NewCircuitBreakerPipeline(mock, threshold, resetTimeout, WithClock(mockClock))
			call := &llm.FunctionCall{Name: tt.toolName}

			var lastResult tools.ToolResult
			for i := 0; i < tt.executions; i++ {
				lastResult = cbPipeline.ExecuteTool(context.Background(), call)
			}

			// If we expected it to open, the next call should instantly fail with ErrToolCircuitOpen
			if tt.wantFinalErr == tools.ErrToolCircuitOpen {
				res := cbPipeline.ExecuteTool(context.Background(), call)
				if res.Error == nil || !errors.Is(res.Error, tools.ErrToolCircuitOpen) {
					t.Errorf("expected circuit open error, got: %v", res.Error)
				}
			} else {
				if lastResult.Error != nil && tt.wantFinalErr == nil {
					t.Errorf("expected success, got error: %v", lastResult.Error)
				}
			}

			if tt.waitTimeout {
				mockClock.Advance(resetTimeout * 2)
				res := cbPipeline.ExecuteTool(context.Background(), call)
				// Should have transitioned to half-open, meaning it actually called the mock again
				if res.Error == nil || errors.Is(res.Error, tools.ErrToolCircuitOpen) {
					t.Errorf("expected mock error after wait (half-open state), got: %v", res.Error)
				}
			}
		})
	}
}

func TestCircuitBreakerPipeline_Recovery(t *testing.T) {
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
