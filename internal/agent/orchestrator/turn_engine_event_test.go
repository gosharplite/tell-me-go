// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

func TestTurnEngine_EventPublishFailure(t *testing.T) {
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelError}))
	badBus := &agenttest.MockEventBusFail{PublishErr: errors.New("simulated publish failure")}

	strategy := session.NewContextStrategy(&agenttest.MockTokenCounter{})
	cm := session.NewContextManager(strategy, nil, badBus, nil)

	Turn := &orchestrator.Turn{
		Events:     badBus,
		Logger:     testLogger,
		CtxManager: cm,
		State:      &orchestrator.TurnState{LastError: errors.New("dummy error")},
		Clock:      clock.RealClock{},
	}

	// Test Recovery Step (simulating a transient error so it attempts a retry)
	transientErr := orchestrator.NewAgentError(llm.ErrTransient, "transient issue", nil)
	Turn.State.LastError = transientErr
	rs := &orchestrator.RecoveryStep{Policy: &orchestrator.DefaultRetryPolicy{MaxRetries: 3, Backoff: 1 * time.Millisecond}}
	_, _ = rs.Process(context.Background(), Turn)

	if !strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("RecoveryStep expected event_publish_failed log, got: %s", buf.String())
	}

	buf.Reset()

	// Test Guard Step
	gs := &orchestrator.GuardStep{}
	_, _ = gs.Process(context.Background(), Turn)

	if !strings.Contains(buf.String(), "event_publish_failed") {
		t.Errorf("GuardStep expected event_publish_failed log, got: %s", buf.String())
	}
}
