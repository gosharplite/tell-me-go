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
			expectedErrSubstr: "Panic detected",
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
