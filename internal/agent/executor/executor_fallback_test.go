// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_ErrGroupFallback(t *testing.T) {
	reg := &mockToolRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "test_tool"},
			}
		},
	}

	// Inject a mocked execution plan that returns an explicit error
	// without signaling the channels or canceling the context.
	injectedErr := errors.New("mock worker error")
	mockPlan := func(e *Orchestrator, ctx context.Context, calls []*llm.FunctionCall, resChan chan<- toolExecResult, declinedMap map[int]bool) error {
		// Populate the results cleanly so collector.Wait exits without error
		resChan <- toolExecResult{
			index: 0,
			name:  "test_tool",
			tr:    tools.ToolResult{Text: "mocked success"},
		}
		// Return the error synchronously
		return injectedErr
	}

	exec, err := NewOrchestrator(reg, nil, nil, &ports.NoOpLogger{}, &MockLogger{}, withExecutionPlan(mockPlan))
	require.NoError(t, err)
	t.Cleanup(exec.Shutdown)

	respContent := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "test_tool"}},
		},
	}

	_, execErr := exec.Execute(context.Background(), respContent, 0, 10)

	// Because collector.Wait returns nil (all results are in),
	// waitErr == nil is true. Then g.Wait() returns injectedErr,
	// so waitErr is updated to injectedErr.
	require.ErrorIs(t, execErr, injectedErr)
}
