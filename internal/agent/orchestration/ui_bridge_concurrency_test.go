// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/goleak"
)

func TestUIBridge_StressConcurrency(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
	defer bridge.Cleanup()

	var activeSpinners int32

	// Thread-safe mock setup with atomic tracking
	mRenderer.On("StartSpinnerWithMetrics", mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
		atomic.AddInt32(&activeSpinners, 1) // Increment when the mock is called
	}).Return(func() {
		atomic.AddInt32(&activeSpinners, -1) // Return the expected func() type for cleanup
	})

	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Maybe()

	const numGoroutines = 100
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	start := make(chan struct{})
	for i := 0; i < numGoroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			<-start
			if idx%2 == 0 {
				bridge.handleEvent(context.Background(), events.ToolExecutionStartedEvent{ToolNames: []string{"test_tool"}})
			} else {
				bridge.handleEvent(context.Background(), events.ResponseEvent{
					Content: &llm.Content{},
				})
			}
		}(i)
	}

	close(start)
	wg.Wait()

	// Final cleanup must stop any remaining spinner
	syncBridge(t, bridge, mRenderer)
	bridge.Cleanup()

	assert.Equal(t, int32(0), atomic.LoadInt32(&activeSpinners), "Every started spinner must be stopped eventually")
}

func TestUIBridge_LogicalStateVerification(t *testing.T) {
	defer goleak.VerifyNone(t)
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(context.Background(), mRenderer, true, true, false, true, "log.txt", slog.Default())
	defer bridge.Cleanup()

	spinnerStopped := make(chan struct{})
	started := make(chan struct{})
	canFinishStart := make(chan struct{})
	stoppedChan := make(chan struct{})

	// This mock simulates the "act" phase being slow
	mRenderer.On("StartSpinnerWithMetrics", mock.Anything, " Executing tools...").Run(func(args mock.Arguments) {
		close(started)
		<-canFinishStart
	}).Return(func() {
		close(spinnerStopped)
		close(stoppedChan)
	}).Once()

	mRenderer.On("RenderResponse", mock.Anything, mock.Anything, mock.Anything).Return().Once()

	// 1. Start ToolExecution in a goroutine
	go func() {
		bridge.handleEvent(context.Background(), events.ToolExecutionStartedEvent{})
	}()

	// Wait until StartSpinnerWithMetrics is called and blocked
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for spinner to start")
	}

	// 2. Process ResponseEvent while ToolExecution is in its "act" phase (unlocked)
	bridge.handleEvent(context.Background(), events.ResponseEvent{
		Content: &llm.Content{},
	})

	// 3. Allow ToolExecution to finish its "act" phase and re-lock
	close(canFinishStart)

	// 4. Wait for the spinner to be stopped
	select {
	case <-stoppedChan:
	case <-time.After(1 * time.Second):
		t.Fatal("Timeout waiting for spinner to be stopped")
	}

	// 5. Verify the spinner was immediately stopped
		select {
	case <-spinnerStopped:
	case <-time.After(2 * time.Second):
		t.Error("Spinner started during ResponseEvent processing must be immediately stopped")
	}
}
