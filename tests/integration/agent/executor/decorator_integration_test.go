// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

type mockExecutionObserver struct{}

func (m *mockExecutionObserver) ExecutionTimedOut(toolID string)      {}
func (m *mockExecutionObserver) ExecutionCompletedLate(toolID string) {}

func registerMockTool(reg tools.Registry, name string) error {
	decl := &tools.ToolDeclaration{
		Name:        name,
		Description: "A mock tool that sleeps to test timeouts",
		Parameters: &tools.Schema{
			Type: "object",
			Properties: map[string]*tools.Schema{
				"timeout": {Type: "number"},
			},
		},
	}

	handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		select {
		case <-time.After(5 * time.Second):
			return tools.ToolResult{Text: "completed"}, nil
		case <-ctx.Done():
			return tools.ToolResult{Error: ctx.Err()}, nil
		}
	}

	return reg.Register(decl, handler)
}

func TestIntegration_DecoratorKillsProcess(t *testing.T) {
	t.Parallel()

	// 1. Setup real dependencies
	reg := agenttest.NewMockToolRegistry()
	sm := &testutil.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true) // Bypass prompts
	logger := &ports.NoOpLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bus := events.NewSimpleEventBus(ctx)
	observer := &mockExecutionObserver{}

	// 2. Register local mock tools instead of the real workspace tools
	err := registerMockTool(reg, "execute_command")
	require.NoError(t, err)

	// 3. Initialize the test using the primary public port/factory
	dispatcher, err := executor.NewPipelineDispatcher(reg, sm, bus, logger, observer)
	require.NoError(t, err)

	// 4. Setup args (Decorator parses timeout, Tool runs command)
	call := &llm.FunctionCall{
		Name: "execute_command",
		Args: map[string]interface{}{
			"command": "sleep 5",
			"reason":  "testing decorator timeout integration",
			"timeout": 1, // dynamically overrides the 300s default
		},
	}

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: call},
		},
	}

	start := time.Now()

	// 5. Execute
	resContent, err := dispatcher.Execute(context.Background(), content, 0, 10)
	elapsed := time.Since(start)

	// 6. Verify actual termination via elapsed time
	require.NoError(t, err, "dispatcher itself shouldn't fail fatally")
	require.Less(t, elapsed, 3*time.Second, "Execution was not terminated by the decorator's context")

	// 7. Verify domain boundary (Error translation via string content and events)
	require.NotNil(t, resContent)
	require.Len(t, resContent.Parts, 1)
	part := resContent.Parts[0]

	require.NotNil(t, part.FunctionResponse)
	resError := part.FunctionResponse.Response["result"]
	require.NotNil(t, resError)
	resErrorStr := resError.(string)

	// The safety decorator prefixes transient errors with "timed out after"
	require.True(t, strings.Contains(resErrorStr, "timed out after") || strings.Contains(resErrorStr, llm.ErrTransient.Error()),
		"Result text should indicate a transient timeout error, got: %s", resErrorStr)
}

func TestIntegration_DecoratorKillsPipeline(t *testing.T) {
	t.Parallel()

	// 1. Setup real dependencies
	reg := agenttest.NewMockToolRegistry()
	sm := &testutil.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true) // Bypass prompts
	logger := &ports.NoOpLogger{}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	bus := events.NewSimpleEventBus(ctx)
	observer := &mockExecutionObserver{}

	// 2. Register local mock tools instead of the real workspace tools
	err := registerMockTool(reg, "pipe_commands")
	require.NoError(t, err)

	// 3. Initialize the test using the primary public port/factory
	dispatcher, err := executor.NewPipelineDispatcher(reg, sm, bus, logger, observer)
	require.NoError(t, err)

	// 4. Setup args
	call := &llm.FunctionCall{
		Name: "pipe_commands",
		Args: map[string]interface{}{
			"commands": []string{"sleep 5", "cat"},
			"reason":   "testing decorator timeout integration for pipes",
			"timeout":  1,
		},
	}

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: call},
		},
	}

	start := time.Now()

	// 5. Execute
	resContent, err := dispatcher.Execute(context.Background(), content, 0, 10)
	elapsed := time.Since(start)

	// 6. Verify actual termination via elapsed time
	require.NoError(t, err, "dispatcher itself shouldn't fail fatally")
	require.Less(t, elapsed, 3*time.Second, "Execution was not terminated by the decorator's context")

	// 7. Verify domain boundary (Error translation)
	require.NotNil(t, resContent)
	require.Len(t, resContent.Parts, 1)
	part := resContent.Parts[0]

	require.NotNil(t, part.FunctionResponse)
	resError := part.FunctionResponse.Response["result"]
	require.NotNil(t, resError)
	resErrorStr := resError.(string)

	require.True(t, strings.Contains(resErrorStr, "timed out after") || strings.Contains(resErrorStr, llm.ErrTransient.Error()),
		"Result text should indicate a transient timeout error, got: %s", resErrorStr)
}
