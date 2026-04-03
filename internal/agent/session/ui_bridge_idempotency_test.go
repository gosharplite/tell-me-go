// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestUIBridge_Cleanup_Idempotent(t *testing.T) {
	t.Parallel()
	mRenderer := new(mockUIRenderer)
	bridge := newUIBridge(mRenderer,
		withBridgeThoughts(true),
		withBridgeTools(true),
		withBridgeRawOutput(false),
		withBridgeColor(true),
		withBridgeLogFile("log.txt"),
		withBridgeLogger(slog.Default()),
		withBridgeCleanupTimeout(10*time.Millisecond),
	)
	bridge.Start(context.Background())

	const numCalls = 100
	var wg sync.WaitGroup
	wg.Add(numCalls)

	// Trigger 100 concurrent Cleanup calls
	for i := 0; i < numCalls; i++ {
		go func() {
			defer wg.Done()
			bridge.Cleanup()
		}()
	}

	// Wait for all Cleanup calls to return
	wg.Wait()

	// ASSERTION 1: Background logic only ran once
	assert.Equal(t, int32(1), atomic.LoadInt32(&bridge.cleanupInvocations), "Cleanup logic should only execute once regardless of how many times Cleanup() is called")

	// Final cleanup to allow goroutines to exit gracefully
	bridge.CloseInput()
}
