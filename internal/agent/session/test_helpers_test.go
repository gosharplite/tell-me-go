// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

func syncBridge(t *testing.T, b *ui.Bridge, m *agenttest.MockUIRenderer) {
	t.Helper()
	done := make(chan struct{})
	prev := m.LogSystemMessageFn
	m.LogSystemMessageFn = func(ctx context.Context, msg string, level string) {
		if prev != nil {
			prev(ctx, msg, level)
		}
		if msg == "SYNC_SENTINEL" {
			close(done)
		}
	}
	defer func() { m.LogSystemMessageFn = prev }()

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
