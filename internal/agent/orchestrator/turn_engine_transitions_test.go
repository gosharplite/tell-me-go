// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator_test

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
)

func TestTurnEngine_ExecutionStep_NoToolCalls(t *testing.T) {
	t.Parallel()
	step := &orchestrator.ExecutionStep{}
	tn := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			HasToolCalls: false,
		},
		Clock: &agenttest.MockClock{},
	}

	res, err := step.Process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != orchestrator.PhasePersisting {
		t.Errorf("expected phase %s, got %s", orchestrator.PhasePersisting, res.NextPhase)
	}
}

func TestTurnEngine_RecoveryStep_NoLastError(t *testing.T) {
	t.Parallel()
	step := &orchestrator.RecoveryStep{}
	tn := &orchestrator.Turn{
		State: &orchestrator.TurnState{
			LastError: nil,
		},
		Clock: &agenttest.MockClock{},
	}

	res, err := step.Process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != orchestrator.PhaseComplete {
		t.Errorf("expected phase %s, got %s", orchestrator.PhaseComplete, res.NextPhase)
	}
}
