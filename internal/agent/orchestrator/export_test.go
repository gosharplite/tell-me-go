// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import "context"

// Exported for external tests (if any remain that aren't already exported in main files)

func ExecutePhase(e *Engine, ctx context.Context, turn *Turn) (ProcessResult, error) {
	return e.executePhase(ctx, turn)
}

func ValidatePayloadLimits(p *ExecutionStep, ctx context.Context, turn *Turn) {
	p.validatePayloadLimits(ctx, turn)
}

func (e *Engine) WithMetrics() TurnMiddleware {
	return e.withMetrics()
}

func (e *Engine) WithStatusReporter() TurnMiddleware {
	return e.withStatusReporter()
}

func (e *Engine) PrepareNextTurn(turn *Turn) {
	e.prepareNextTurn(turn)
}

func (e *Engine) EmergencySave(turn *Turn) {
	e.emergencySave(turn)
}
