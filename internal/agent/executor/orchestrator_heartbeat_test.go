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

func TestOrchestrator_HeartbeatTimeout(t *testing.T) {
	t.Parallel()

	hangingTool := &tools.ToolDeclaration{
		Name:        "heartbeat_tool",
		Description: "I emit heartbeats then hang",
	}

	reg := &mockZombieRegistry{
		isLongRunningFn: func(name string) bool { return true },
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
			
			time.Sleep(50 * time.Millisecond)
			
			select {
			case hb <- struct{}{}:
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			}
			
			// Now hang longer than the threshold (100ms)
			select {
			case <-ctx.Done():
				return tools.ToolResult{Text: "cancelled"}, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return tools.ToolResult{Text: "done"}, nil
			}
		},
	}

	// Orchestrator with safety decorator
	exec, err := NewOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{}, 
		withToolTimeout(1*time.Second),
		WithLongRunningTimeout(2*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: hangingTool.Name}
	tool, _ := exec.resolver.Resolve(fc)

	// Execute
	result, err := exec.runtime.Execute(ctx, tool, fc, nil)

	// Should have timed out due to heartbeat missing
	assert.Error(t, result.Error)
	assert.True(t, strings.Contains(result.Error.Error(), "timed out") || strings.Contains(result.Error.Error(), "canceled"), "Error should be timeout/canceled, got: %v", result.Error)
}

func TestOrchestrator_HeartbeatSuccess(t *testing.T) {
	t.Parallel()

	livelyTool := &tools.ToolDeclaration{
		Name:        "lively_tool",
		Description: "I emit heartbeats and finish successfully",
	}

	reg := &mockZombieRegistry{
		isLongRunningFn: func(name string) bool { return true },
		livenessThreshold: 100 * time.Millisecond,
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{livelyTool}
		},
		executeFn: func(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
			// Emit heartbeats every 50ms for 200ms (total 4 heartbeats)
			for i := 0; i < 4; i++ {
				select {
				case hb <- struct{}{}:
				case <-ctx.Done():
					return tools.ToolResult{Text: "cancelled"}, ctx.Err()
				}
				time.Sleep(50 * time.Millisecond)
			}
			return tools.ToolResult{Text: "success"}, nil
		},
	}

	exec, err := NewOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{},
		withToolTimeout(1*time.Second),
		WithLongRunningTimeout(2*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	ctx := context.Background()
	fc := &llm.FunctionCall{Name: livelyTool.Name}
	tool, _ := exec.resolver.Resolve(fc)

	// Execute
	result, err := exec.runtime.Execute(ctx, tool, fc, nil)

	// Should succeed because heartbeats kept it alive
	assert.NoError(t, err)
	assert.NoError(t, result.Error)
	assert.Equal(t, "success", result.Text)
}
