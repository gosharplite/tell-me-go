// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/mock"
)

func syncBridge(t *testing.T, b *ui.Bridge, m interface {
	On(methodName string, arguments ...interface{}) *mock.Call
}) {
	t.Helper()
	// Use a sentinel event that is handled by the bridge and calls a mock method.
	// LogSystemMessage is ideal as it's safe to call when no spinner is active.
	done := make(chan struct{})
	m.On("LogSystemMessage", mock.Anything, "SYNC_SENTINEL", "info").Run(func(_ mock.Arguments) {
		close(done)
	}).Return().Once()

	// Use a non-polling send via HandleEvent. SystemMessageEvent is critical
	// and will be delivered with backpressure.
	if err := b.HandleEvent(context.Background(), events.SystemMessageEvent{Message: "SYNC_SENTINEL", Level: "info"}); err != nil {
		t.Fatalf("Failed to queue sync sentinel: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for sync sentinel processing")
	}
}
