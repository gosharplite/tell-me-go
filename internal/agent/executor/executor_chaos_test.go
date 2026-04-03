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

func TestDispatcher_ChaosScenarios(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		mockSetup     func(syncChan chan struct{}) *mockToolPipeline
		calls         []*llm.FunctionCall
		wantPanicText string // or specific error assertion
		wantTimeout   bool
	}{
		{
			name: "parallel tool panics mid-flight",
			mockSetup: func(syncChan chan struct{}) *mockToolPipeline {
				return &mockToolPipeline{
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
			mockSetup: func(syncChan chan struct{}) *mockToolPipeline {
				return &mockToolPipeline{
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
			mockSetup: func(syncChan chan struct{}) *mockToolPipeline {
				return &mockToolPipeline{
					IsSerialFunc: func(n string) bool { return false },
					ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
						if call.Name == "hang_tool" {
							// Signal that the tool has started
							close(syncChan)
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
			t.Parallel()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			syncChan := make(chan struct{})
			mock := tt.mockSetup(syncChan)

			// Setup minimal dispatcher state
			cfg := dispatcherConfig{
				MaxConcurrentTools: 5,
				ToolTimeout:        1 * time.Hour,
			}

			o := &Dispatcher{
				pipeline: mock,
				events:   events.NewSimpleEventBus(context.Background()),
				logger:   &ports.NoOpLogger{},
			}
			o.state.Store(&dispatcherState{
				config: cfg,
			})

			declinedMap := make(map[int]bool)
			results := make([]tools.ToolResult, len(tt.calls))

			if tt.wantTimeout {
				assertTimeoutScenario(t, o, ctx, tt.calls, declinedMap, results, syncChan, cancel)
				return // Test successful
			}

			// For non-timeout tests, run synchronously
			assertPanicScenario(t, o, ctx, tt.calls, declinedMap, results, tt.wantPanicText)
		})
	}
}

func assertTimeoutScenario(t *testing.T, o *Dispatcher, ctx context.Context, calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult, syncChan chan struct{}, cancel context.CancelFunc) {
	t.Helper()
	errChan := make(chan error, 1)
	go func() {
		errChan <- o.runExecutionPlan(ctx, calls, declinedMap, results)
	}()

	<-syncChan
	cancel()

	err := <-errChan
	if err == nil {
		t.Errorf("expected context cancellation error, got nil")
	}
}

func assertPanicScenario(t *testing.T, o *Dispatcher, ctx context.Context, calls []*llm.FunctionCall, declinedMap map[int]bool, results []tools.ToolResult, wantPanicText string) {
	t.Helper()
	err := o.runExecutionPlan(ctx, calls, declinedMap, results)
	if err != nil {
		t.Logf("runExecutionPlan returned error: %v", err)
	}

	if wantPanicText != "" {
		assertPanicResult(t, results, wantPanicText)
	} else {
		assertNoErrorResult(t, results)
	}
}

func assertPanicResult(t *testing.T, results []tools.ToolResult, wantPanicText string) {
	t.Helper()
	for _, res := range results {
		if res.Error != nil && strings.Contains(res.Error.Error(), wantPanicText) {
			return
		}
		if strings.Contains(res.Text, wantPanicText) {
			return
		}
	}

	t.Errorf("expected to find panic text %q in results, got none", wantPanicText)
	for i, r := range results {
		t.Logf("Result %d: Text=%q, Err=%v", i, r.Text, r.Error)
	}
}

func assertNoErrorResult(t *testing.T, results []tools.ToolResult) {
	t.Helper()
	for i, res := range results {
		if res.Error != nil {
			t.Errorf("unexpected error for call %d: %v", i, res.Error)
		}
	}
}
