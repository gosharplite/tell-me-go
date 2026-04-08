// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"testing"
)

func TestTurnEngine_ExecutionStep_NoToolCalls(t *testing.T) {
	t.Parallel()
	step := &executionStep{}
	tn := &Turn{
		State: &TurnState{
			HasToolCalls: false,
		},
		Clock: &mockClock{},
	}

	res, err := step.Process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != PhasePersisting {
		t.Errorf("expected phase %s, got %s", PhasePersisting, res.NextPhase)
	}
}

func TestTurnEngine_RecoveryStep_NoLastError(t *testing.T) {
	t.Parallel()
	step := &recoveryStep{}
	tn := &Turn{
		State: &TurnState{
			LastError: nil,
		},
		Clock: &mockClock{},
	}

	res, err := step.Process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != PhaseComplete {
		t.Errorf("expected phase %s, got %s", PhaseComplete, res.NextPhase)
	}
}
