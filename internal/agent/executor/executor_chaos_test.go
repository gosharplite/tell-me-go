package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestOrchestrator_ChaosScenarios(t *testing.T) {
	tests := []struct {
		name          string
		mockSetup     func() *MockToolPipeline
		calls         []*llm.FunctionCall
		wantPanicText string // or specific error assertion
		wantTimeout   bool
	}{
		{
			name: "parallel tool panics mid-flight",
			mockSetup: func() *MockToolPipeline {
				return &MockToolPipeline{
					IsSerialFunc: func(n string) bool { return false },
					ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
						if call.Name == "dangerous_tool" {
							panic("simulated third-party crash")
						}
						return tools.ToolResult{Text: "safe"}
					},
					RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
						m := make(map[int]bool)
						for i := range calls {
							m[i] = true
						}
						return ctx, m
					},
				}
			},
			calls: []*llm.FunctionCall{
				{Name: "safe_tool_1"},
				{Name: "dangerous_tool"},
				{Name: "safe_tool_2"},
			},
			wantPanicText: "simulated third-party crash",
		},
		{
			name: "serial tool panics mid-flight",
			mockSetup: func() *MockToolPipeline {
				return &MockToolPipeline{
					IsSerialFunc: func(n string) bool { return true },
					ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
						panic("simulated serial crash")
					},
					RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
						return ctx, map[int]bool{0: true}
					},
				}
			},
			calls: []*llm.FunctionCall{
				{Name: "dangerous_serial"},
			},
			wantPanicText: "simulated serial crash",
		},
		{
			name: "context deadline exceeded during fan-in",
			mockSetup: func() *MockToolPipeline {
				return &MockToolPipeline{
					IsSerialFunc: func(n string) bool { return false },
					ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
						if call.Name == "hang_tool" {
							// Block until context is canceled
							<-ctx.Done()
							return tools.ToolResult{Text: "aborted", Error: ctx.Err()}
						}
						return tools.ToolResult{Text: "safe"}
					},
					RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
						m := make(map[int]bool)
						for i := range calls {
							m[i] = true
						}
						return ctx, m
					},
				}
			},
			calls: []*llm.FunctionCall{
				{Name: "safe_tool_1"},
				{Name: "hang_tool"},
			},
			wantTimeout: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := tt.mockSetup()

			// Setup minimal orchestrator state
			cfg := OrchestratorConfig{
				MaxConcurrentTools: 5,
				ToolTimeout:        100 * time.Millisecond,
			}

			o := &Orchestrator{
				pipeline: mock,
				events:   events.NewSimpleEventBus(context.Background()),
				logger:   &ports.NoOpLogger{},
			}
			o.state.Store(&orchestratorState{
				config: cfg,
			})
			t.Cleanup(func() {
			})

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			if !tt.wantTimeout {
				// if we don't expect a timeout from the test, give it more time
				// wait, but waitTimeout=false means we don't test context timeout
				// Let's use a long timeout for tests that shouldn't timeout
				cancel()
				ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
			}
			defer cancel()

			declinedMap := make(map[int]bool)
			results := make([]tools.ToolResult, len(tt.calls))

			err := o.runExecutionPlan(ctx, tt.calls, declinedMap, results)

			if tt.wantTimeout {
				if err == nil {
					t.Errorf("expected context cancellation error, got nil")
				}
				return // Nothing more to check for timeout
			}

			// For panic tests, verify we didn't deadlock and the result captures the panic string
			if err != nil {
				t.Logf("runExecutionPlan returned error (could be acceptable depending on panic): %v", err)
			}

			foundPanic := false
			for i, res := range results {
				if tt.wantPanicText != "" {
					if res.Error != nil && strings.Contains(res.Error.Error(), tt.wantPanicText) {
						foundPanic = true
					} else if strings.Contains(res.Text, tt.wantPanicText) {
						foundPanic = true
					}
				} else {
					if res.Error != nil {
						t.Errorf("unexpected error for call %d: %v", i, res.Error)
					}
				}
			}

			if tt.wantPanicText != "" && !foundPanic {
				t.Errorf("expected to find panic text %q in results, got none", tt.wantPanicText)
				for i, r := range results {
					t.Logf("Result %d: Text=%q, Err=%v", i, r.Text, r.Error)
				}
			}
		})
	}
}
