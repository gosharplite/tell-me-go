// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

func TestTurnEngine_EventPublishFailure(t *testing.T) {
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	badBus := &mockEventBusFail{publishErr: errors.New("simulated publish failure")}

	strategy := session.NewContextStrategy(&mockTokenCounter{})
	cm := session.NewContextManager(strategy, nil, badBus, nil)

	turn := &Turn{
		Events:     badBus,
		Logger:     testLogger,
		CtxManager: cm,
		State:      &TurnState{LastError: errors.New("dummy error")},
		Clock:      clock.RealClock{},
	}

	// Test Recovery Step (simulating a transient error so it attempts a retry)
	transientErr := NewAgentError(llm.ErrTransient, "transient issue", nil)
	turn.State.LastError = transientErr
	rs := &recoveryStep{Policy: &defaultRetryPolicy{MaxRetries: 3, Backoff: 1 * time.Millisecond}}
	_, _ = rs.Process(context.Background(), turn)

	if !strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("RecoveryStep expected event_publish_failed log, got: %s", buf.String())
	}

	buf.Reset()

	// Test Guard Step
	gs := &guardStep{}
	_, _ = gs.Process(context.Background(), turn)

	if !strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("GuardStep expected event_publish_failed log, got: %s", buf.String())
	}
}
