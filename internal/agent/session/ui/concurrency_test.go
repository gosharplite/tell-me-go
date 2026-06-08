// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestUIBridge_StressConcurrency(t *testing.T) {
	t.Parallel()
	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()

	var activeSpinners int32

	// Thread-safe mock setup with atomic tracking
	mRenderer.StartSpinnerWithMetricsFn = func(ctx context.Context, status string) func() {
		current := atomic.AddInt32(&activeSpinners, 1)
		assert.LessOrEqual(t, current, int32(1), "Actor model violation: Concurrent spinners detected")
		return func() {
			atomic.AddInt32(&activeSpinners, -1)
		}
	}

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	start := make(chan struct{})
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			if idx%2 == 0 {
				_ = bridge.HandleEvent(context.Background(), events.ToolExecutionStartedEvent{ToolNames: []string{"test_tool"}})
			} else {
				_ = bridge.HandleEvent(context.Background(), events.ResponseEvent{
					Content: &llm.Content{},
				})
			}
		}(i)
	}

	close(start)
	wg.Wait()

	// Final cleanup must stop any remaining spinner
	syncBridge(t, bridge, mRenderer)
	bridge.CloseInput()
	bridge.Cleanup()

	assert.Equal(t, int32(0), atomic.LoadInt32(&activeSpinners), "Every started spinner must be stopped eventually")
}

func TestUIBridge_LogicalStateVerification(t *testing.T) {
	t.Parallel()
	testCtx := context.Background()

	mRenderer := new(agenttest.MockUIRenderer)
	bridge := NewBridge(mRenderer, WithBridgeThoughts(true), WithBridgeTools(true), WithBridgeRawOutput(false), WithBridgeColor(true), WithBridgeLogFile("log.txt"), WithBridgeLogger(slog.Default()))
	_, _, _ = startListen(t, bridge)
	bridge.WaitStarted()
	defer func() {
		bridge.CloseInput()
		bridge.Cleanup()
	}()

	// 1. Expect the spinner to start first
	// Note: startSpinnerForPhase(ToolExecutionStartedEvent) calls StartSpinnerWithMetrics
	var metricsCalled, renderCalled int32
	mRenderer.StartSpinnerWithMetricsFn = func(ctx context.Context, status string) func() {
		atomic.AddInt32(&metricsCalled, 1)
		return func() {}
	}

	// 2. Expect the response to be rendered sequentially after
	mRenderer.RenderResponseFn = func(ctx context.Context, content *llm.Content, showThoughts, rawOutput bool) {
		atomic.AddInt32(&renderCalled, 1)
	}

	// 3. Queue the events sequentially
	_ = bridge.HandleEvent(testCtx, events.ToolExecutionStartedEvent{})
	_ = bridge.HandleEvent(testCtx, events.ResponseEvent{Content: &llm.Content{}})

	// 4. Flush the queue using the robust syncBridge helper
	syncBridge(t, bridge, mRenderer)

	// 5. Verify the sequential execution happened as expected
	assert.Equal(t, int32(1), atomic.LoadInt32(&metricsCalled), "StartSpinnerWithMetrics should be called exactly once")
	assert.Equal(t, int32(1), atomic.LoadInt32(&renderCalled), "RenderResponse should be called exactly once")
}
