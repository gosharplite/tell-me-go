// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_HeartbeatTimeout(t *testing.T) {
	t.Parallel()

	hangingTool := &tools.ToolDeclaration{
		Name:        "heartbeat_tool",
		Description: "I emit heartbeats then hang",
	}

	tickCh := make(chan struct{})

	reg := &mockZombieRegistry{
		isLongRunningFn:   func(name string) bool { return true },
		livenessThreshold: 100 * time.Millisecond,
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{hangingTool}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			// Emit 2 heartbeats
			select {
			case hb <- struct{}{}:
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			}

			select {
			case <-tickCh:
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			}

			select {
			case hb <- struct{}{}:
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			}

			// Now hang — wait for context cancellation (no more ticks)
			select {
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return tools.ToolResult{Text: "done"}, nil
			}
		},
	}

	// Dispatcher with safety decorator
	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{},
		WithToolTimeout(1*time.Second),
		WithLongRunningTimeout(2*time.Second),
	)
	require.NoError(t, err)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: hangingTool.Name}
	tool, _ := exec.pipeline.(*defaultToolPipeline).resolver.Resolve(fc)

	// Execute in goroutine so we can feed ticks
	doneCh := make(chan struct{})
	var result tools.ToolResult
	var execErr error
	go func() {
		defer close(doneCh)
		result, execErr = exec.pipeline.(*defaultToolPipeline).runtime.Execute(ctx, tool, fc, nil)
	}()

	// Feed 1 tick to let the second heartbeat through, then stop
	tickCh <- struct{}{}

	<-doneCh
	require.NoError(t, execErr)

	// Should have timed out due to heartbeat missing
	assert.Error(t, result.Error)
	assert.True(t, strings.Contains(result.Error.Error(), "timed out") || strings.Contains(result.Error.Error(), "canceled"), "Error should be timeout/canceled, got: %v", result.Error)
}

func TestDispatcher_HeartbeatSuccess(t *testing.T) {
	t.Parallel()

	livelyTool := &tools.ToolDeclaration{
		Name:        "lively_tool",
		Description: "I emit heartbeats and finish successfully",
	}

	tickCh := make(chan struct{})

	reg := &mockZombieRegistry{
		isLongRunningFn:   func(name string) bool { return true },
		livenessThreshold: 500 * time.Millisecond,
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{livelyTool}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			// Emit 4 heartbeats, each gated by a tick from the test
			for i := 0; i < 4; i++ {
				select {
				case hb <- struct{}{}:
				case <-ctx.Done():
					return tools.ToolResult{Text: "cancelled"}, ctx.Err()
				}
				select {
				case <-tickCh:
				case <-ctx.Done():
					return tools.ToolResult{Text: "cancelled"}, ctx.Err()
				}
			}
			return tools.ToolResult{Text: "success"}, nil
		},
	}

	exec, err := NewPipelineDispatcher(reg, &mockSecurityManager{AllowAll: true}, &mockEventBus{}, &ports.NoOpLogger{}, &mockLogger{},
		WithToolTimeout(1*time.Second),
		WithLongRunningTimeout(2*time.Second),
	)
	require.NoError(t, err)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: livelyTool.Name}
	tool, _ := exec.pipeline.(*defaultToolPipeline).resolver.Resolve(fc)

	// Execute in goroutine so we can feed ticks
	doneCh := make(chan struct{})
	var result tools.ToolResult
	var execErr error
	go func() {
		defer close(doneCh)
		result, execErr = exec.pipeline.(*defaultToolPipeline).runtime.Execute(ctx, tool, fc, nil)
	}()

	// Feed 4 ticks to allow all heartbeats
	for i := 0; i < 4; i++ {
		tickCh <- struct{}{}
	}

	<-doneCh
	require.NoError(t, execErr)

	// Should succeed because heartbeats kept it alive
	assert.NoError(t, result.Error)
	assert.Equal(t, "success", result.Text)
}
