// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
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

func TestTurnEngine_ValidateTurnLimit(t *testing.T) {
	e := &TurnEngine{
		ctxManager: &ContextManager{
			Strategy: &ContextStrategy{},
		},
	}
	e.ctxManager.Strategy.SetLimits(1000, 5, 10)

	ctx := context.Background()
	// Turn 5 is allowed
	if err := e.validateTurn(ctx, 5); err != nil {
		t.Errorf("expected no error for turn 5, got %v", err)
	}

	// Turn 6 is NOT allowed
	err := e.validateTurn(ctx, 6)
	if err == nil {
		t.Error("expected error for turn 6, got nil")
	} else if !errors.Is(err, ErrMaxTurnsReached) {
		t.Errorf("expected ErrMaxTurnsReached, got %v", err)
	}
}
