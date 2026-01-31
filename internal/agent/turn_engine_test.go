// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"
)

func TestTurnEngine_ValidateTurn(t *testing.T) {
	e := &TurnEngine{
		ctxManager: &ContextManager{
			Strategy: &ContextStrategy{},
		},
	}
	e.ctxManager.Strategy.SetLimits(1000, 5, 10)

	ctx := context.Background()
	if err := e.validateTurn(ctx, 0); err != nil {
		t.Errorf("expected no error for turn 0, got %v", err)
	}

	cancelledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := e.validateTurn(cancelledCtx, 0); err == nil {
		t.Error("expected error for cancelled context, got nil")
	}
}
