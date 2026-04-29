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
