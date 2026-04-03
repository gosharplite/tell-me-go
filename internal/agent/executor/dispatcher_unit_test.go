// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcher_BatchingAndConcurrency(t *testing.T) {
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
	observer := &mockLogger{}

	// Create Dispatcher but replace runtime with mockExecutor
	exec, err := NewPipelineDispatcher(reg, nil, bus, logger, observer)
	require.NoError(t, err)

	releaseCh := make(chan struct{})
	var startedCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(5)

	mock := &mockExecutor{
		Result:  tools.ToolResult{Text: "done"},
		Delay:   1, // Trigger block
		BlockCh: releaseCh,
		ExecuteHook: func() {
			startedCount.Add(1)
			wg.Done()
		},
	}
	exec.pipeline.(*CircuitBreakerPipeline).next.(*defaultToolPipeline).runtime = mock

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "p1"}},
			{FunctionCall: &llm.FunctionCall{Name: "p2"}},
			{FunctionCall: &llm.FunctionCall{Name: "p3"}},
			{FunctionCall: &llm.FunctionCall{Name: "p4"}},
			{FunctionCall: &llm.FunctionCall{Name: "p5"}},
		},
	}

	// Wait for all 5 to be started (indicating they are parallel)
	go func() {
		wg.Wait()
		close(releaseCh)
	}()

	resp, err := exec.Execute(context.Background(), content, 0, 10)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parts, 5)

	assert.Equal(t, int32(5), startedCount.Load(), "Expected 5 tools to have started in parallel")
}

func TestDispatcher_SerialBatching(t *testing.T) {
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
	observer := &mockLogger{}

	// Create Dispatcher but replace runtime with mockExecutor
	exec, err := NewPipelineDispatcher(reg, nil, bus, logger, observer)
	require.NoError(t, err)

	releaseCh1 := make(chan struct{})
	releaseCh2 := make(chan struct{})
	var startedCount atomic.Int32

	mock := &mockExecutor{
		Result: tools.ToolResult{Text: "done"},
		Delay:  1, // Trigger block
		ExecuteHook: func() {
			count := startedCount.Add(1)
			switch count {
			case 1:
				close(releaseCh1)
			case 2:
				close(releaseCh2)
			}
		},
	}
	// Note: mock.BlockCh is problematic if we want different ones for different calls.
	// Let's use a smarter hook or just update BlockCh dynamically if possible.
	// Actually, the current mockExecutor only has one BlockCh.
	// Let's refactor mockExecutor to support a channel provider or just use a single one that we close in stages?
	// No, once closed, it's closed.

	// Better: use a channel per call?
	// Let's use a simpler approach for serial:
	// The first call starts, we wait for it to be "started", then we release it.
	// THEN we wait for second call to start.

	currentReleaseCh := make(chan struct{})
	mock.BlockCh = currentReleaseCh

	exec.pipeline.(*CircuitBreakerPipeline).next.(*defaultToolPipeline).runtime = mock

	content := &llm.Content{
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{Name: "s1"}},
			{FunctionCall: &llm.FunctionCall{Name: "s2"}},
		},
	}

	go func() {
		// Wait for first call
		<-releaseCh1
		// First call is active, verify second HAS NOT started
		assert.Equal(t, int32(1), startedCount.Load())
		// Release first call
		close(currentReleaseCh)

		// Wait for second call
		<-releaseCh2
		// Second call is active
		assert.Equal(t, int32(2), startedCount.Load())
		// Wait, we closed currentReleaseCh, so the second call will also unblock immediately.
		// That's fine for serial verification.
	}()

	resp, err := exec.Execute(context.Background(), content, 0, 10)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Parts, 2)
}
