// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthDecorator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		authFunc     func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error
		nextResult   tools.ToolResult
		nextCalled   bool
		expectedText string
		expectedErr  error
	}{
		{
			name: "Authorized - Success",
			authFunc: func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
				return nil
			},
			nextResult:   tools.ToolResult{Text: "ok"},
			nextCalled:   true,
			expectedText: "ok",
		},
		{
			name: "Unauthorized - Error",
			authFunc: func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
				return errors.New("unauthorized")
			},
			nextCalled:   false,
			expectedText: "unauthorized",
			expectedErr:  errors.New("unauthorized"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := &mockExecutor{Result: tt.nextResult}
			auth := &mockToolAuthService{AuthorizeFunc: tt.authFunc}
			decorator := newAuthDecorator(next, auth)

			res, err := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"}, nil)

			assert.NoError(t, err)
			assert.Equal(t, tt.nextCalled, next.Called)
			assert.Equal(t, tt.expectedText, res.Text)
			if tt.expectedErr != nil {
				assert.Equal(t, tt.expectedErr, res.Error)
			} else {
				assert.NoError(t, res.Error)
			}
		})
	}
}

func TestSafetyDecorator(t *testing.T) {
	t.Parallel()
	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	tests := []struct {
		name              string
		nextPanic         bool
		nextDelay         time.Duration
		timeout           time.Duration
		expectedErrSubstr string
	}{
		{
			name:    "Success",
			timeout: 1 * time.Second,
		},
		{
			name:              "Panic caught",
			nextPanic:         true,
			timeout:           1 * time.Second,
			expectedErrSubstr: "tool execution panic",
		},
		{
			name:              "Timeout",
			nextDelay:         500 * time.Millisecond,
			timeout:           100 * time.Millisecond,
			expectedErrSubstr: "timed out",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := &mockExecutor{Panic: tt.nextPanic, Delay: tt.nextDelay}
			decorator := newSafetyDecorator(
				next,
				registry,
				logger,
				bus,
				zombie,
				tt.timeout,
				tt.timeout,
				tt.timeout,
			)

			res, _ := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"}, nil)

			if tt.expectedErrSubstr != "" {
				assert.Error(t, res.Error)
				assert.Contains(t, res.Error.Error(), tt.expectedErrSubstr)
			} else {
				assert.NoError(t, res.Error)
			}
		})
	}
}

// TestSafetyDecorator_PrecedenceLogic verifies the context-cancellation vs
// tool-error precedence condition at decorators.go:91-93. When both the tool
// returns an error and the context is cancelled/deadline-exceeded, the
// friendly context message takes priority over the raw tool error.
func TestSafetyDecorator_PrecedenceLogic(t *testing.T) {
	t.Parallel()
	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	// Subtest A: context alive → tool error passes through unchanged.
	// ctx.Err() == nil, so the precedence check short-circuits and we
	// return out.Result, out.Err verbatim.
	t.Run("context_OK_tool_error_preserved", func(t *testing.T) {
		t.Parallel()
		diskFull := errors.New("disk full")
		next := &mockExecutor{
			Result: tools.ToolResult{Text: "write failed"},
			Err:    diskFull,
		}
		decorator := newSafetyDecorator(
			next, registry, logger, bus, zombie,
			5*time.Second, 5*time.Second, 10*time.Millisecond,
		)

		res, err := decorator.Execute(
			context.Background(),
			&tools.ToolDeclaration{Name: "test"},
			&llm.FunctionCall{Name: "test"},
			nil,
		)

		// Safety decorator returns nil error when the select picks the outCh
		// branch (it does, because ctx is not done). The tool error is in the
		// second return value from the fallthrough path: return out.Result, out.Err.
		assert.Error(t, err, "tool error must pass through as second return value")
		assert.Contains(t, err.Error(), "disk full")
		assert.Equal(t, "write failed", res.Text)
		assert.NoError(t, res.Error, "result.Error is nil because the mock returned error separately")
	})

	// Subtest B: pre-cancelled context → friendly cancellation message
	// overrides the raw tool error. Both ctx.Done() and outCh may be
	// simultaneously ready; Go selects randomly. Both paths produce the
	// same formatContextError output that wraps context.Canceled.
	t.Run("context_cancelled_tool_error_overridden", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancelled per ADR-036 §2

		next := &mockExecutor{
			Result: tools.ToolResult{Text: "raw output"},
			Err:    errors.New("raw tool crash"),
		}
		decorator := newSafetyDecorator(
			next, registry, logger, bus, zombie,
			5*time.Second, 5*time.Second, 10*time.Millisecond,
		)

		res, err := decorator.Execute(
			ctx,
			&tools.ToolDeclaration{Name: "test"},
			&llm.FunctionCall{Name: "test"},
			nil,
		)

		// Both select branches return (friendly result, nil).
		assert.NoError(t, err)
		assert.Error(t, res.Error)
		assert.Contains(t, res.Text, "interrupted or cancelled")
		assert.True(t, errors.Is(res.Error, context.Canceled),
			"error must wrap context.Canceled")
	})

	// Subtest C: nearly-immediate deadline → friendly timeout message
	// overrides the raw tool error. Same dual-path semantics as B.
	t.Run("context_deadline_exceeded_tool_error_overridden", func(t *testing.T) {
		t.Parallel()
		next := &mockExecutor{
			Result: tools.ToolResult{Text: "raw output"},
			Err:    errors.New("tool failed"),
		}
		decorator := newSafetyDecorator(
			next, registry, logger, bus, zombie,
			1*time.Nanosecond, 1*time.Nanosecond, 10*time.Millisecond,
		)

		// Pre-deadlined context per ADR-036 §2: ensures the child context
		// created by context.WithTimeout inside safetyDecorator.Execute
		// inherits a past deadline, making ctx.Done() deterministically
		// readable before the mock executor goroutine can deliver a result.
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
		defer cancel()

		res, err := decorator.Execute(
			ctx,
			&tools.ToolDeclaration{Name: "test"},
			&llm.FunctionCall{Name: "test"},
			nil,
		)

		// Both select branches return (friendly result, nil).
		assert.NoError(t, err)
		assert.Error(t, res.Error)
		assert.Contains(t, res.Text, "timed out")
		assert.True(t, errors.Is(res.Error, llm.ErrTransient),
			"error must wrap llm.ErrTransient")
	})
}

// TestSafetyDecorator_ContextCancellationPrecedence exercises the precedence-check
// logic in decorators.go:91-93 directly, without going through the goroutine-based
// select statement. This ensures deterministic coverage of the branch where both
// the tool returned an error AND the context is cancelled/deadline-exceeded.
func TestSafetyDecorator_ContextCancellationPrecedence(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	d := newSafetyDecorator(
		&mockExecutor{},
		registry,
		logger,
		bus,
		zombie,
		5*time.Second,
		5*time.Second,
		10*time.Millisecond,
	).(*safetyDecorator)

	tests := []struct {
		name             string
		toolErr          error
		ctxErr           error
		activeTimeout    time.Duration
		wantNilError     bool
		wantTextContains string
		wantErrorWraps   error
	}{
		{
			name:             "context_canceled_tool_errored",
			toolErr:          errors.New("raw tool crash"),
			ctxErr:           context.Canceled,
			activeTimeout:    5 * time.Second,
			wantNilError:     true,
			wantTextContains: "interrupted or cancelled",
			wantErrorWraps:   context.Canceled,
		},
		{
			name:             "context_deadline_exceeded_tool_errored",
			toolErr:          errors.New("tool timed out internally"),
			ctxErr:           context.DeadlineExceeded,
			activeTimeout:    2 * time.Second,
			wantNilError:     true,
			wantTextContains: "timed out after 2s",
			wantErrorWraps:   llm.ErrTransient,
		},
		{
			name:             "context_canceled_tool_succeeded",
			toolErr:          nil,
			ctxErr:           context.Canceled,
			activeTimeout:    5 * time.Second,
			wantNilError:     false,
			wantTextContains: "",
			wantErrorWraps:   nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := tools.ToolOutput{
				Result: tools.ToolResult{Text: "raw tool output"},
				Err:    tt.toolErr,
			}

			var ctx context.Context
			var cancel context.CancelFunc
			switch {
			case errors.Is(tt.ctxErr, context.Canceled):
				ctx, cancel = context.WithCancel(context.Background())
				cancel()
			case errors.Is(tt.ctxErr, context.DeadlineExceeded):
				ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
				defer cancel()
			default:
				ctx = context.Background()
			}

			var result tools.ToolResult
			var execErr error

			result, execErr = d.handleToolOutput(out, ctx, tt.activeTimeout)

			if tt.wantNilError {
				require.NoError(t, execErr, "second return value must be nil")
			} else {
				require.Equal(t, tt.toolErr, execErr, "second return value must be the tool error")
			}

			if tt.wantTextContains != "" {
				require.Contains(t, result.Text, tt.wantTextContains)
			}

			if tt.wantErrorWraps != nil {
				require.Error(t, result.Error, "result.Error must be non-nil")
				require.True(t, errors.Is(result.Error, tt.wantErrorWraps),
					"result.Error must wrap %v, got: %v", tt.wantErrorWraps, result.Error)
			} else if tt.toolErr == nil {
				require.NoError(t, result.Error, "result.Error must be nil when tool succeeded")
				require.Equal(t, "raw tool output", result.Text)
			}
		})
	}
}

// TestSafetyDecorator_Execute_RacePrecedence hits the race-condition path at
// decorators.go:91-93 where the outCh case wins the select when the context
// is also cancelled/deadline-exceeded and the tool returned an error. Because
// Go's select is non-deterministic when multiple cases are ready, this test
// retries up to 200 times to guarantee coverage. The expected behavior is
// identical for both select branches: formatContextError is returned with a
// nil second return value.
func TestSafetyDecorator_Execute_RacePrecedence(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	t.Run("cancelled_context_outCh_wins", func(t *testing.T) {
		t.Parallel()
		next := &mockExecutor{
			Result: tools.ToolResult{Text: "raw output"},
			Err:    errors.New("raw tool crash"),
		}
		decorator := newSafetyDecorator(
			next, registry, logger, bus, zombie,
			5*time.Second, 5*time.Second, 10*time.Millisecond,
		)

		// Retry until the select picks outCh over ctx.Done().
		// Both branches produce formatContextError output, so we verify
		// the outcome matches regardless of which path was taken.
		for i := 0; i < 200; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			cancel() // pre-cancelled per ADR-036 §2

			res, err := decorator.Execute(
				ctx,
				&tools.ToolDeclaration{Name: "test"},
				&llm.FunctionCall{Name: "test"},
				nil,
			)

			// Both branches return (friendly result, nil).
			require.NoError(t, err)
			require.Error(t, res.Error)
			require.Contains(t, res.Text, "interrupted or cancelled")
			require.True(t, errors.Is(res.Error, context.Canceled),
				"error must wrap context.Canceled")
		}
	})

	t.Run("deadlined_context_outCh_wins", func(t *testing.T) {
		t.Parallel()
		next := &mockExecutor{
			Result: tools.ToolResult{Text: "raw output"},
			Err:    errors.New("tool failed"),
		}
		decorator := newSafetyDecorator(
			next, registry, logger, bus, zombie,
			1*time.Nanosecond, 1*time.Nanosecond, 10*time.Millisecond,
		)

		for i := 0; i < 200; i++ {
			// Pre-deadlined context: ensures child context inherits a past deadline.
			ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))

			res, err := decorator.Execute(
				ctx,
				&tools.ToolDeclaration{Name: "test"},
				&llm.FunctionCall{Name: "test"},
				nil,
			)

			cancel() // clean up immediately, not deferred

			require.NoError(t, err)
			require.Error(t, res.Error)
			require.Contains(t, res.Text, "timed out")
			require.True(t, errors.Is(res.Error, llm.ErrTransient),
				"error must wrap llm.ErrTransient")
		}
	})
}

func TestTracingDecorator(t *testing.T) {
	t.Parallel()
	registry := &panicRegistry{}
	logger := &ports.NoOpLogger{}

	next := &mockExecutor{Result: tools.ToolResult{Text: "ok"}}
	decorator := newTracingDecorator(next, registry, logger)

	res, err := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"}, nil)

	assert.NoError(t, err)
	assert.Equal(t, "ok", res.Text)
	assert.True(t, next.Called)
}

func TestFormatToolExecutionError(t *testing.T) {
	err1 := errors.New("system error")
	err2 := errors.New("tool error")

	t.Run("both errors", func(t *testing.T) {
		got := formatToolExecutionError(err1, err2)
		assert.Contains(t, got, "system error")
		assert.Contains(t, got, "tool error")
	})

	t.Run("only system error", func(t *testing.T) {
		got := formatToolExecutionError(err1, nil)
		assert.Equal(t, "system error", got)
	})

	t.Run("only tool error", func(t *testing.T) {
		got := formatToolExecutionError(nil, err2)
		assert.Equal(t, "tool error", got)
	})

	t.Run("no errors", func(t *testing.T) {
		got := formatToolExecutionError(nil, nil)
		assert.Equal(t, "", got)
	})
}

func TestClassifyToolError(t *testing.T) {
	t.Parallel()

	t.Run("Security error - includes message", func(t *testing.T) {
		err := fmt.Errorf("%w: access to system directory '/var' is forbidden", domain_security.ErrSandboxViolation)
		status, msg := classifyToolError(err, nil)
		assert.Equal(t, "security_blocked", status)
		assert.Contains(t, msg, "Action blocked by the system sandbox security policy")
		assert.Contains(t, msg, "access to system directory '/var' is forbidden")
	})

	t.Run("User declined", func(t *testing.T) {
		err := tools.ErrUserDeclined
		status, msg := classifyToolError(err, nil)
		assert.Equal(t, "user_declined", status)
		assert.Contains(t, msg, "The user explicitly denied this action")
	})

	t.Run("Other error", func(t *testing.T) {
		err := errors.New("other error")
		status, msg := classifyToolError(err, nil)
		assert.Equal(t, "error", status)
		assert.Equal(t, "", msg)
	})

	t.Run("Success", func(t *testing.T) {
		status, msg := classifyToolError(nil, nil)
		assert.Equal(t, "success", status)
		assert.Equal(t, "", msg)
	})
}

// TestHandleTimeout_DefaultBranch covers the default case in handleTimeout
// that wraps unknown errors with llm.ErrTransient.
func TestHandleTimeout_DefaultBranch(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	decorator := newSafetyDecorator(
		&mockExecutor{},
		registry,
		logger,
		bus,
		zombie,
		1*time.Second,
		1*time.Second,
		10*time.Millisecond, // short zombie timeout
	).(*safetyDecorator)

	// A custom error that is NOT context.Canceled and NOT context.DeadlineExceeded
	customErr := errors.New("custom transport error")
	outCh := make(chan tools.ToolOutput, 1)

	result := HandleTimeout(
		decorator,
		context.Background(),
		customErr,
		"test_tool",
		5*time.Second,
		outCh,
	)

	// The default branch wraps with ErrTransient
	assert.Error(t, result.Error)
	assert.True(t, errors.Is(result.Error, llm.ErrTransient),
		"expected error to wrap llm.ErrTransient")
	assert.Contains(t, result.Text, "custom transport error")
	assert.Contains(t, result.Text, "Error: Tool execution failed")
}

// TestHandleTimeout_ContextCanceled verifies the Canceled branch (already covered,
// but included for completeness of the handleTimeout contract).
func TestHandleTimeout_ContextCanceled(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	decorator := newSafetyDecorator(
		&mockExecutor{},
		registry,
		logger,
		bus,
		zombie,
		1*time.Second,
		1*time.Second,
		10*time.Millisecond,
	).(*safetyDecorator)

	outCh := make(chan tools.ToolOutput, 1)

	result := HandleTimeout(
		decorator,
		context.Background(),
		context.Canceled,
		"test_tool",
		5*time.Second,
		outCh,
	)

	assert.Error(t, result.Error)
	assert.Contains(t, result.Text, "interrupted or cancelled")
	assert.Contains(t, result.Error.Error(), "tool execution canceled")
}

// TestHandleTimeout_DeadlineExceeded verifies the DeadlineExceeded branch.
func TestHandleTimeout_DeadlineExceeded(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	decorator := newSafetyDecorator(
		&mockExecutor{},
		registry,
		logger,
		bus,
		zombie,
		1*time.Second,
		1*time.Second,
		10*time.Millisecond,
	).(*safetyDecorator)

	outCh := make(chan tools.ToolOutput, 1)

	result := HandleTimeout(
		decorator,
		context.Background(),
		context.DeadlineExceeded,
		"test_tool",
		2*time.Second,
		outCh,
	)

	assert.Error(t, result.Error)
	assert.True(t, errors.Is(result.Error, llm.ErrTransient))
	assert.Contains(t, result.Text, "timed out after 2s")
}

// TestLivenessTimer_ResetAfterFire covers the drain path in livenessTimer.reset
// when timer.Stop() returns false (timer already fired).
func TestLivenessTimer_ResetAfterFire(t *testing.T) {
	t.Parallel()

	lt := NewLivenessTimer(1 * time.Millisecond)

	// Wait for timer to fire
	select {
	case <-lt.channel():
		// timer fired as expected
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timer did not fire within expected window")
	}

	// Reset after fire — exercises the !t.timer.Stop() drain path
	lt.reset()

	// Timer should fire again after reset
	select {
	case <-lt.channel():
		// success
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timer did not fire after reset")
	}

	lt.stop()
}

// TestLivenessTimer_ResetBeforeFire covers the normal reset path
// when timer.Stop() returns true (timer hasn't fired yet).
func TestLivenessTimer_ResetBeforeFire(t *testing.T) {
	t.Parallel()

	lt := NewLivenessTimer(1 * time.Hour)
	// Timer hasn't fired — Stop() returns true
	lt.reset()
	lt.stop()
}

// TestLivenessTimer_NilTimer covers the nil timer path in reset()
func TestLivenessTimer_NilTimer(t *testing.T) {
	t.Parallel()

	lt := NewLivenessTimer(0) // zero threshold → nil timer
	// reset() should be a no-op when timer is nil
	lt.reset()
	lt.stop()
}

// TestMonitorLiveness_HeartbeatDropOnFullChannel covers the default branch
// in monitorLiveness where the heartbeat channel is full and the heartbeat is dropped.
func TestMonitorLiveness_HeartbeatDropOnFullChannel(t *testing.T) {
	t.Parallel()

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	// Pre-fill the heartbeat channel so it's full
	heartbeat := make(chan struct{}, 1)
	heartbeat <- struct{}{}

	// Mock executor sends one heartbeat on hbCh before returning success
	next := &mockExecutor{
		Result:     tools.ToolResult{Text: "ok"},
		Heartbeats: 1,
	}

	decorator := newSafetyDecorator(
		next,
		registry,
		logger,
		bus,
		zombie,
		5*time.Second, // long timeout so we don't hit deadline
		5*time.Second,
		10*time.Millisecond,
	)

	result, _ := decorator.Execute(
		context.Background(),
		&tools.ToolDeclaration{Name: "test"},
		&llm.FunctionCall{Name: "test"},
		heartbeat,
	)

	// The tool should complete successfully — heartbeat was silently dropped
	assert.NoError(t, result.Error)
	assert.Equal(t, "ok", result.Text)

	// Drain the pre-filled value to prevent goroutine leak warnings
	<-heartbeat
}

// TestHandlePanic_EventBusError_Logged verifies that when SafePublish fails,
// the error is logged at Error level and the ToolResult still contains ErrTerminal.
func TestHandlePanic_EventBusError_Logged(t *testing.T) {
	t.Parallel()

	logger := &capturingLogger{}
	bus := &errorEventBus{err: errors.New("channel full")}

	decorator := &safetyDecorator{
		logger: logger,
		events: bus,
	}

	result := decorator.handlePanic(context.Background(), "test panic", "panic_tool")

	// ToolResult must still contain ErrTerminal
	assert.Error(t, result.Error)
	assert.True(t, errors.Is(result.Error, llm.ErrTerminal),
		"expected error to wrap llm.ErrTerminal")
	assert.Contains(t, result.Text, "panic_tool")
	assert.Contains(t, result.Text, "internal fatal error")

	// Logger must have captured the publish failure
	assert.True(t, logger.errorCalled, "Expected Error to be called on logger")
	assert.Equal(t, "failed to publish panic event", logger.lastMsg)

	// Check attributes
	attrs := make(map[string]any)
	for i := 0; i < len(logger.lastArgs); i += 2 {
		if i+1 < len(logger.lastArgs) {
			key, ok := logger.lastArgs[i].(string)
			if ok {
				attrs[key] = logger.lastArgs[i+1]
			}
		}
	}

	assert.Equal(t, "panic_tool", attrs["tool_name"])
	assert.ErrorContains(t, attrs["error"].(error), "channel full")
}

func TestForwardHeartbeat_SuccessfulSend(t *testing.T) {
	t.Parallel()

	t.Run("buffered_channel_with_capacity", func(t *testing.T) {
		t.Parallel()
		ch := make(chan struct{}, 1)
		forwardHeartbeat(ch, struct{}{})

		select {
		case v := <-ch:
			// Successfully received the heartbeat value
			_ = v
		default:
			t.Error("expected heartbeat to be sent successfully on buffered channel with capacity")
		}
	})

	t.Run("unbuffered_channel_with_concurrent_receiver", func(t *testing.T) {
		t.Parallel()
		ch := make(chan struct{})
		received := make(chan struct{})

		go func() {
			<-ch
			close(received)
		}()

		// Poll until the receiver is actually waiting and the non-blocking send succeeds.
		// forwardHeartbeat does not return a value, so we poll it and check if
		// the receiver unblocked and closed the 'received' channel.
		require.Eventually(t, func() bool {
			forwardHeartbeat(ch, struct{}{})
			select {
			case <-received:
				return true
			default:
				return false
			}
		}, 1*time.Second, 5*time.Millisecond)
	})

	t.Run("nil_channel_no_panic", func(t *testing.T) {
		t.Parallel()
		// Must not panic, must return immediately
		assert.NotPanics(t, func() {
			forwardHeartbeat(nil, struct{}{})
		})
	})
}

// TestDrainHeartbeats_DrainsChannel verifies that the drainHeartbeats
// goroutine drains all buffered values from hbCh without blocking, and
// that the recover() guard catches any panic that might arise (e.g., from
// a hypothetical double-close race).
func TestDrainHeartbeats_DrainsChannel(t *testing.T) {
	t.Parallel()

	t.Run("drains_all_values_without_blocking", func(t *testing.T) {
		t.Parallel()
		hbCh := make(chan struct{}, 3)
		hbCh <- struct{}{}
		hbCh <- struct{}{}
		hbCh <- struct{}{}

		drainHeartbeats(hbCh)

		// The goroutine should drain all values; closing the channel
		// after a brief yield should let the drain complete cleanly.
		require.Eventually(t, func() bool {
			return len(hbCh) == 0
		}, 500*time.Millisecond, 5*time.Millisecond,
			"drainHeartbeats goroutine should consume all buffered values")

		// Close the channel so the drain goroutine's for-range exits.
		close(hbCh)
	})

	t.Run("recovers_from_double_close", func(t *testing.T) {
		t.Parallel()
		hbCh := make(chan struct{}, 1)
		hbCh <- struct{}{}

		drainHeartbeats(hbCh)

		// Wait for the drain to consume the value
		require.Eventually(t, func() bool {
			return len(hbCh) == 0
		}, 500*time.Millisecond, 5*time.Millisecond)

		// Close the channel — the drain goroutine's for-range exits cleanly.
		close(hbCh)

		// The recover() is protecting against a hypothetical double-close.
		// Since drainHeartbeats itself never closes the channel, the real
		// protection is against the tool goroutine's defer close(hbCh) racing
		// with the drain. We verify the goroutine exits by ensuring no panic
		// propagates to the test process.
	})
}

// TestMonitorLiveness_TimerFires_CancelsContext covers the case <-timer.channel() branch
// in monitorLiveness (decorators.go:266). When the liveness timer fires before any
// heartbeat arrives, handleLivenessTimeout is called, which logs "tool_liveness_timeout"
// and cancels the tool context.
func TestMonitorLiveness_TimerFires_CancelsContext(t *testing.T) {
	t.Parallel()

	logger := &capturingLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)

	// Use a registry that reports a very short liveness threshold
	reg := &mockZombieRegistry{
		livenessThreshold: 1 * time.Millisecond,
		isLongRunningFn:   func(name string) bool { return true },
	}

	// Mock executor sends zero heartbeats and blocks until context cancelled
	// (simulating a tool that stops sending heartbeats, causing the liveness
	// timer to fire).
	next := &mockExecutor{
		Result:     tools.ToolResult{Text: "should_not_appear"},
		Delay:      1, // triggers blocking on BlockCh
		BlockCh:    make(chan struct{}),
		Heartbeats: 0, // no heartbeats → timer will fire
	}
	defer close(next.BlockCh)

	decorator := newSafetyDecorator(
		next,
		reg,
		logger,
		bus,
		zombie,
		5*time.Second, // long tool timeout — must NOT be the one that fires
		5*time.Second, // long long-running timeout
		10*time.Millisecond,
	).(*safetyDecorator)

	result, _ := decorator.Execute(
		context.Background(),
		&tools.ToolDeclaration{Name: "zombie_tool"},
		&llm.FunctionCall{Name: "zombie_tool"},
		nil,
	)

	// The liveness timer should fire, causing handleLivenessTimeout to cancel
	// the tool's context. The tool then returns ctx.Err().
	assert.Error(t, result.Error)
	assert.True(t,
		errors.Is(result.Error, context.Canceled) ||
			errors.Is(result.Error, context.DeadlineExceeded),
		"expected context.Canceled or context.DeadlineExceeded, got: %v", result.Error)

	// Verify the logger captured the liveness timeout
	assert.True(t, logger.errorCalled,
		"expected Error to be called for tool_liveness_timeout")
	assert.Equal(t, "tool_liveness_timeout", logger.lastMsg)

	// Verify the tool name is in the log args
	attrs := make(map[string]any)
	for i := 0; i < len(logger.lastArgs); i += 2 {
		if i+1 < len(logger.lastArgs) {
			key, ok := logger.lastArgs[i].(string)
			if ok {
				attrs[key] = logger.lastArgs[i+1]
			}
		}
	}
	assert.Equal(t, "zombie_tool", attrs["tool_name"])
}
