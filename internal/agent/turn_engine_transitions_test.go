// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"
)

func TestTurnEngine_ExecutionStep_NoToolCalls(t *testing.T) {
	step := &executionStep{}
	tn := &turn{
		State: &turnState{
			HasToolCalls: false,
		},
	}

	res, err := step.process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != phasePersisting {
		t.Errorf("expected phase %s, got %s", phasePersisting, res.NextPhase)
	}
}

func TestTurnEngine_RecoveryStep_NoLastError(t *testing.T) {
	step := &recoveryStep{}
	tn := &turn{
		State: &turnState{
			LastError: nil,
		},
	}

	res, err := step.process(context.Background(), tn)

	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res.NextPhase != phaseComplete {
		t.Errorf("expected phase %s, got %s", phaseComplete, res.NextPhase)
	}
}
