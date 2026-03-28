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
	"github.com/stretchr/testify/require"
)

func TestOrchestrator_BatchingAndConcurrency(t *testing.T) {
	t.Parallel()

	// Registry that defines tools as parallel
	reg := &mockZombieRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "p1"},
				{Name: "p2"},
				{Name: "p3"},
				{Name: "p4"},
				{Name: "p5"},
			}
		},
	}

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &MockLogger{}

	// Create Orchestrator but replace runtime with mockExecutor
	exec, err := NewOrchestrator(reg, nil, bus, logger, observer)
	require.NoError(t, err)
	defer exec.Shutdown()

	mock := &mockExecutor{
		Result: tools.ToolResult{Text: "done"},
		Delay:  50 * time.Millisecond,
	}
	exec.runtime = mock

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "p1"}},
			{FunctionCall: &llm.FunctionCall{Name: "p2"}},
			{FunctionCall: &llm.FunctionCall{Name: "p3"}},
			{FunctionCall: &llm.FunctionCall{Name: "p4"}},
			{FunctionCall: &llm.FunctionCall{Name: "p5"}},
		},
	}

	start := time.Now()
	resp, err := exec.Execute(context.Background(), content, 0, 10)
	duration := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parts, 5)

	// Since they run in parallel (max 5), it should take roughly 50ms + overhead
	assert.True(t, duration < 200*time.Millisecond, "Expected parallel execution but it took too long")
}

func TestOrchestrator_SerialBatching(t *testing.T) {
	t.Parallel()

	// Registry that defines tools as serial
	reg := &mockZombieRegistry{
		getDeclarationsFn: func() []*tools.ToolDeclaration {
			return []*tools.ToolDeclaration{
				{Name: "s1"},
				{Name: "s2"},
			}
		},
		isSerialFn: func(name string) bool { return true },
	}

	logger := &ports.NoOpLogger{}
	bus := &mockEventBus{}
	observer := &MockLogger{}

	// Create Orchestrator but replace runtime with mockExecutor
	exec, err := NewOrchestrator(reg, nil, bus, logger, observer)
	require.NoError(t, err)
	defer exec.Shutdown()

	mock := &mockExecutor{
		Result: tools.ToolResult{Text: "done"},
		Delay:  50 * time.Millisecond,
	}
	exec.runtime = mock

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "s1"}},
			{FunctionCall: &llm.FunctionCall{Name: "s2"}},
		},
	}

	start := time.Now()
	resp, err := exec.Execute(context.Background(), content, 0, 10)
	duration := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parts, 2)

	// Since they run in serial, it should take at least 100ms
	assert.True(t, duration >= 100*time.Millisecond, "Expected serial execution but it took too short")
}
