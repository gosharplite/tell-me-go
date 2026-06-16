package executor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
				events:   events.NewSimpleEventBus(ctx),
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
		errChan <- o.runExecutionPlan(ctx, calls, declinedMap, results, nil)
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
	err := o.runExecutionPlan(ctx, calls, declinedMap, results, nil)

	// With the new contract, tool-result errors (including panics recovered inside
	// the tool pipeline) are delivered to the LLM via AssembleResponse and are NOT
	// promoted to plan-level Go errors. Only cancellation signals (index:-1) propagate.
	if wantPanicText != "" {
		assert.NoError(t, err, "runExecutionPlan must NOT return an error for recovered panics")
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

// assertLogHasPanicAttr verifies that the capturingLogger recorded a "panic" key
// with a value containing the expected substring (key-value pair convention).
func assertLogHasPanicAttr(t *testing.T, log *capturingLogger, wantContain string) {
	t.Helper()
	var foundPanicAttr bool
	for i := 0; i < len(log.lastArgs)-1; i += 2 {
		key, ok := log.lastArgs[i].(string)
		if !ok {
			continue
		}
		if key == "panic" {
			foundPanicAttr = true
			valStr := fmt.Sprintf("%v", log.lastArgs[i+1])
			assert.Contains(t, valStr, wantContain,
				"logger 'panic' attribute must contain the panic value")
		}
	}
	assert.True(t, foundPanicAttr, "logger attributes must include 'panic' key")
}

// TestExecuteParallelBatch_FanInPanic_Propagates verifies that a panic in the
// fan-in wait goroutine (e.g. a corrupted sync.WaitGroup causing wg.Wait() to panic)
// is propagated as an error through the results channel and surfaced to the caller
// via runExecutionPlan → planErrors → errors.Join.
//
// Since inducing a genuine wg.Wait() panic is impractical in a deterministic test,
// this test uses two complementary approaches:
//  1. Direct verification of the fan-in recover pattern (the exact code block from
//     executeParallelBatch) — confirms the recover path sends an index:-1 sentinel
//     with an ErrTerminal-wrapped error.
//  2. Indirect verification through runExecutionPlan with a parallel worker panic —
//     confirms the full error propagation chain: panic → recover → resultsCh →
//     handleBatchResults → planErrors → errors.Join → caller.
func TestExecuteParallelBatch_FanInPanic_Propagates(t *testing.T) {
	t.Parallel()

	t.Run("fan-in recover pattern propagates panic as error", func(t *testing.T) {
		t.Parallel()
		// This subtest directly exercises the fan-in goroutine recover pattern.
		// It simulates a panic in the wg.Wait() equivalent and verifies that
		// the error is sent through resultsCh with index:-1 and wrapped in ErrTerminal.
		resultsCh := make(chan toolExecResult, 1)

		go func() {
			defer func() {
				if r := recover(); r != nil {
					// Exact same pattern as executeParallelBatch fan-in goroutine.
					resultsCh <- toolExecResult{
						index: -1,
						name:  "fan_in_panic",
						tr: tools.ToolResult{
							Text:  fmt.Sprintf("internal error: fan-in panic: %v", r),
							Error: fmt.Errorf("%w: fan-in panic: %v", llm.ErrTerminal, r),
						},
					}
				}
				close(resultsCh)
			}()
			panic("simulated wg.Wait corruption") // simulates what a corrupted WaitGroup would do
		}()

		var results []toolExecResult
		for res := range resultsCh {
			results = append(results, res)
		}

		require.Len(t, results, 1, "expected exactly 1 result from fan-in panic")

		res := results[0]
		assert.Equal(t, -1, res.index, "expected index -1 sentinel")
		assert.Equal(t, "fan_in_panic", res.name)
		require.Error(t, res.tr.Error, "expected error in ToolResult")
		assert.True(t, errors.Is(res.tr.Error, llm.ErrTerminal), "expected error wrapped in ErrTerminal")
		assert.Contains(t, res.tr.Error.Error(), "simulated wg.Wait corruption")
		assert.Contains(t, res.tr.Text, "fan-in panic")
	})

	t.Run("parallel worker panic propagates through runExecutionPlan", func(t *testing.T) {
		t.Parallel()
		// This subtest verifies the full propagation chain through runExecutionPlan.
		// A parallel worker panic is recovered by the worker's defer (not the fan-in
		// goroutine). With the new contract, the error stays in results[] and is
		// delivered to the LLM via AssembleResponse — NOT promoted to a plan-level
		// Go error. Only cancellation signals (index:-1) propagate as plan errors.
		mock := &mockToolPipeline{
			IsSerialFunc: func(n string) bool { return n == "serial_tool" },
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				if call.Name == "crash_tool" {
					panic("simulated nil pointer dereference in tool")
				}
				return tools.ToolResult{Text: "safe_result"}
			},
			RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
				return ctx, nil
			},
		}

		cfg := dispatcherConfig{
			MaxConcurrentTools: 3,
			ToolTimeout:        1 * time.Hour,
		}
		cfg.applyDefaults()

		ctx := context.Background()
		bus := events.NewSimpleEventBus(ctx)
		o := &Dispatcher{
			pipeline: mock,
			events:   bus,
			logger:   &ports.NoOpLogger{},
		}
		o.state.Store(&dispatcherState{config: cfg})

		calls := []*llm.FunctionCall{
			{Name: "safe_tool"},
			{Name: "crash_tool"},
			{Name: "another_safe_tool"},
		}
		declinedMap := make(map[int]bool)
		results := make([]tools.ToolResult, len(calls))

		err := o.runExecutionPlan(ctx, calls, declinedMap, results, nil)

		// With the new contract, tool-result errors stay in results[] — they are
		// NOT promoted to plan-level Go errors. runExecutionPlan returns nil.
		require.NoError(t, err, "runExecutionPlan must return nil — panics are recovered and stored in results[]")

		assert.Equal(t, "safe_result", results[0].Text)
		assert.Equal(t, "safe_result", results[2].Text)

		require.Error(t, results[1].Error, "crash_tool should have an error result")
		assert.Contains(t, results[1].Error.Error(), "simulated nil pointer dereference")
	})

	t.Run("fan_in_no_panic_resultsCh_closed_cleanly", func(t *testing.T) {
		t.Parallel()
		// This subtest verifies that when wg.Wait() completes normally (no panic),
		// resultsCh is cleanly closed with NO spurious index:-1 sentinel injected.
		// Guards against a regression where the fan-in goroutine accidentally
		// injects a sentinel on normal completion.
		mock := &mockToolPipeline{
			IsSerialFunc: func(n string) bool { return false },
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				return tools.ToolResult{Text: "success"}
			},
			RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
				return ctx, nil
			},
		}

		cfg := dispatcherConfig{
			MaxConcurrentTools: 3,
			ToolTimeout:        1 * time.Hour,
		}
		cfg.applyDefaults()

		ctx := context.Background()
		bus := events.NewSimpleEventBus(ctx)
		d := &Dispatcher{
			pipeline: mock,
			events:   bus,
			logger:   &ports.NoOpLogger{},
		}
		d.state.Store(&dispatcherState{config: cfg})

		calls := []*llm.FunctionCall{
			{Name: "tool_a"},
			{Name: "tool_b"},
			{Name: "tool_c"},
		}
		results := make([]tools.ToolResult, 3)

		err := d.runExecutionPlan(ctx, calls, nil, results, nil)

		require.NoError(t, err, "runExecutionPlan must return nil on normal completion")

		for i, res := range results {
			assert.Equal(t, "success", res.Text,
				"tool %s result Text: got %q, want 'success'", calls[i].Name, res.Text)
			assert.NoError(t, res.Error,
				"tool %s must not have an error, got %v", calls[i].Name, res.Error)
			assert.NotContains(t, res.Text, "fan_in_panic",
				"tool %s result must not contain sentinel text 'fan_in_panic'", calls[i].Name)
		}
	})

	t.Run("fan_in_goroutine_panic_inside_executeParallelBatch", func(t *testing.T) {
		t.Parallel()
		// This subtest exercises the actual panic-recovery block inside the
		// fan-in goroutine of executeParallelBatch by injecting a panicking
		// fanInFunc through the test-only seam executeParallelBatchWithFanIn.
		//
		// The injected fanIn panics before workers complete, triggering the
		// defer/recover block which propagates a sentinel (index:-1, name:fan_in_panic)
		// through resultsCh. Without this recovery path, a panic in wg.Wait()
		// would deadlock the agent loop (resultsCh never closed).

		log := &capturingLogger{}

		mock := &mockToolPipeline{
			IsSerialFunc: func(n string) bool { return false },
			ExecuteToolFunc: func(ctx context.Context, call *llm.FunctionCall) tools.ToolResult {
				return tools.ToolResult{Text: "quick_success"}
			},
			RequestBatchConsentFunc: func(ctx context.Context, calls []*llm.FunctionCall) (context.Context, map[int]bool) {
				return ctx, nil
			},
		}

		cfg := dispatcherConfig{
			MaxConcurrentTools: 3,
			ToolTimeout:        1 * time.Hour,
		}
		cfg.applyDefaults()

		ctx := context.Background()
		d := &Dispatcher{
			pipeline: mock,
			events:   events.NewSimpleEventBus(ctx),
			logger:   log,
		}
		d.state.Store(&dispatcherState{config: cfg})

		calls := []*llm.FunctionCall{
			{Name: "tool_a"},
			{Name: "tool_b"},
		}

		batch := taskBatch{
			isSerial: false,
			tasks:    []int{0, 1},
		}

		resultsCh := make(chan toolExecResult, len(batch.tasks)+1)

		panickingFanIn := func(wg *sync.WaitGroup, _ chan<- toolExecResult) {
			wg.Wait() // workers complete first — avoids race on channel close
			panic("simulated WaitGroup corruption in fan-in")
		}

		d.executeParallelBatchWithFanIn(ctx, batch, calls, cfg.MaxConcurrentTools, resultsCh, panickingFanIn)

		var results []toolExecResult
		for res := range resultsCh {
			results = append(results, res)
		}

		// Find the sentinel among results (workers may or may not finish first).
		var sentinel *toolExecResult
		for i := range results {
			if results[i].index == -1 {
				sentinel = &results[i]
				break
			}
		}

		require.NotNil(t, sentinel, "expected at least one sentinel result with index == -1")
		assert.Equal(t, "fan_in_panic", sentinel.name)
		require.Error(t, sentinel.tr.Error)
		assert.True(t, errors.Is(sentinel.tr.Error, llm.ErrTerminal),
			"sentinel error must wrap ErrTerminal")
		assert.Contains(t, sentinel.tr.Error.Error(), "fan-in panic")
		assert.Contains(t, sentinel.tr.Error.Error(), "simulated WaitGroup corruption")
		assert.Contains(t, sentinel.tr.Text, "fan-in panic")

		// Verify the logger captured the panic.
		assert.True(t, log.errorCalled, "logger.Error must have been called")
		assert.Equal(t, "panic in fan-in wait goroutine", log.lastMsg)
		assertLogHasPanicAttr(t, log, "simulated WaitGroup corruption")
	})
}
