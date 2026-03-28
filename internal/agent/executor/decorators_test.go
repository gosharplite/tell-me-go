// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthDecorator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		authFunc      func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error
		nextResult    tools.ToolResult
		nextCalled    bool
		expectedText  string
		expectedErr   error
	}{
		{
			name: "Authorized - Success",
			authFunc: func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
				return nil
			},
			nextResult: tools.ToolResult{Text: "ok"},
			nextCalled: true,
			expectedText: "ok",
		},
		{
			name: "Unauthorized - Error",
			authFunc: func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
				return errors.New("unauthorized")
			},
			nextCalled: false,
			expectedText: "unauthorized",
			expectedErr: errors.New("unauthorized"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := &mockExecutor{Result: tt.nextResult}
			auth := &mockToolAuthService{AuthorizeFunc: tt.authFunc}
			decorator := newAuthDecorator(next, auth)

			res, err := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"})

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

func TestCircuitBreakerDecorator(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		checkErr      error
		nextResult    tools.ToolResult
		nextCalled    bool
		expectedText  string
		expectedErr   error
		recordedSuccess *bool
	}{
		{
			name:     "Circuit Closed - Success",
			checkErr: nil,
			nextResult: tools.ToolResult{Text: "ok"},
			nextCalled: true,
			expectedText: "ok",
			recordedSuccess: func() *bool { b := true; return &b }(),
		},
		{
			name:     "Circuit Open - Failure",
			checkErr: errors.New("circuit open"),
			nextCalled: false,
			expectedText: "circuit open",
			expectedErr: errors.New("circuit open"),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := &mockExecutor{Result: tt.nextResult}
			var lastRecordedSuccess *bool
			cb := &mockCircuitBreakerManager{
				CheckFunc: func(toolName string) error { return tt.checkErr },
				RecordFunc: func(toolName string, success bool) {
					lastRecordedSuccess = &success
				},
			}
			decorator := newCircuitBreakerDecorator(next, cb)

			res, err := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"})

			assert.NoError(t, err)
			assert.Equal(t, tt.nextCalled, next.Called)
			assert.Equal(t, tt.expectedText, res.Text)
			if tt.expectedErr != nil {
				assert.Equal(t, tt.expectedErr, res.Error)
			} else {
				assert.NoError(t, res.Error)
			}
			if tt.recordedSuccess != nil {
				require.NotNil(t, lastRecordedSuccess)
				assert.Equal(t, *tt.recordedSuccess, *lastRecordedSuccess)
			}
		})
	}
}

func TestSafetyDecorator(t *testing.T) {
	t.Parallel()
	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &MockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	tests := []struct {
		name          string
		nextPanic     bool
		nextDelay     time.Duration
		timeout       time.Duration
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
				func() time.Duration { return tt.timeout },
				func() time.Duration { return tt.timeout },
				func() time.Duration { return tt.timeout },
			)

			res, _ := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"})

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

	res, err := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test"})

	assert.NoError(t, err)
	assert.Equal(t, "ok", res.Text)
	assert.True(t, next.Called)
}
