// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
)

func TestSafetyDecorator_DynamicTimeout(t *testing.T) {
	t.Parallel()
	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &mockLogger{}
	zombie, _ := tools.NewZombieTool(observer)
	registry := &panicRegistry{}

	tests := []struct {
		name              string
		requestedTimeout  interface{}
		nextDelay         time.Duration
		defaultTimeout    time.Duration
		expectedErrSubstr string
	}{
		{
			name:             "Override default timeout - Success",
			requestedTimeout: 2, // 2 seconds
			nextDelay:        1 * time.Second,
			defaultTimeout:   500 * time.Millisecond,
		},
		{
			name:              "Override default timeout - Still Times Out",
			requestedTimeout:  1, // 1 second
			nextDelay:         2 * time.Second,
			defaultTimeout:    500 * time.Millisecond,
			expectedErrSubstr: "timed out after 1s",
		},
		{
			name:              "Capped at 2 hours",
			requestedTimeout:  10000, // > 7200
			nextDelay:         100 * time.Millisecond,
			defaultTimeout:    500 * time.Millisecond,
		},
		{
			name:             "Floating point timeout",
			requestedTimeout: 1.5,
			nextDelay:        1 * time.Second,
			defaultTimeout:   500 * time.Millisecond,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			next := &mockExecutor{Result: tools.ToolResult{Text: "ok"}, Delay: tt.nextDelay, BlockCh: make(chan struct{})}
			
			if tt.nextDelay > 0 && tt.expectedErrSubstr == "" {
				timer := time.AfterFunc(tt.nextDelay, func() {
					close(next.BlockCh)
				})
				defer timer.Stop()
			}

			decorator := newSafetyDecorator(
				next,
				registry,
				logger,
				bus,
				zombie,
				tt.defaultTimeout,
				tt.defaultTimeout,
				10 * time.Millisecond, // zombieTimeout (short for testing)
			)

			args := map[string]interface{}{}
			if tt.requestedTimeout != nil {
				args["timeout"] = tt.requestedTimeout
			}

			res, _ := decorator.Execute(context.Background(), &tools.ToolDeclaration{Name: "test"}, &llm.FunctionCall{Name: "test", Args: args}, nil)

			if tt.expectedErrSubstr != "" {
				assert.Error(t, res.Error)
				assert.Contains(t, res.Text, tt.expectedErrSubstr)
			} else {
				assert.NoError(t, res.Error)
			}
		})
	}
}
