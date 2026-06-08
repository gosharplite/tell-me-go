// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionManager_SessionID_DegradationWarning(t *testing.T) {
	mChatter := new(agenttest.MockChatter)
	mCapturer := new(agenttest.MockCapturer)
	mHistory := new(agenttest.MockHistoryManager)
	mEventBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, mEventBus)

	mClock := &agenttest.MockClock{}
	mClock.SetCurrentTime(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC))
	mEntropy := &agenttest.MockEntropySource{
		ReadFunc: func(p []byte) (n int, err error) {
			return 0, fmt.Errorf("os entropy exhaustion")
		},
	}

	var stderr bytes.Buffer

	factory := func(ctx context.Context, deps ports.ChatterComposer, cfg ports.ChatterConfig) (ports.Chatter, error) {
		return mChatter, nil
	}

	mHistoryRenderer := new(agenttest.MockHistoryRenderer)
	mUIRenderer := new(agenttest.MockUIRenderer)
	orch := session.NewSessionManager("home", "1.0.0", nil, io.Discard, &stderr, factory, mHistoryRenderer, mUIRenderer, session.WithClock(mClock), session.WithEntropySource(mEntropy))

	sCfg := session.NewSessionConfig("", false, 0, 0, false, "hello", &config.Config{
		Model: "model",
		Mode:  "mode",
	})
	deps := &agenttest.StubChatterComposer{Paths: &persistence.Paths{}, HistoryManager: mHistory, EventBus: mEventBus, Logger: slog.Default(), TurnsLogger: &ports.NoOpTurnsLogger{}, SessionProvider: new(agenttest.MockSessionProvider)}

	mCapturer.IsTTYFn = func(v any) bool { return true }
	mUIRenderer.SetUseColorFn = func(use bool) {}
	mChatter.SubscribeFn = func(sub func(context.Context, events.Event)) {}
	mChatter.SetLimitsFn = func(ctx context.Context, a, b, c int) error { return nil }
	mChatter.ChatFn = func(ctx context.Context, s *ports.Session, prompt string) error { return nil }
	mChatter.ShutdownFn = func(ctx context.Context) error { return nil }

	err := orch.Run(context.Background(), sCfg, deps, mCapturer)
	require.NoError(t, err)

	assert.Contains(t, stderr.String(), "[WARN] Entropy source failure, degrading to time-based session ID: os entropy exhaustion")
}
