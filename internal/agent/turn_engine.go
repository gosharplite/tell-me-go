// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// TurnHooks allows decoupling UI and logging side effects from the engine.
type TurnHooks struct {
	OnTurnStart   func(turn int)
	OnPrepare     func(metadata *ContextMetadata)
	OnStream      func(ctx context.Context, respCh <-chan *types.Content)
	OnResponse    func(content *types.Content)
	OnToolResults func(results *types.Content)
	OnComplete    func(state *TurnState)
}

// TurnState carries data between the phases of a turn.
type TurnState struct {
	HasToolCalls bool
	Metrics      *types.Metrics
	Tokens       int
	CurrentTurns int
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error)
}

// TurnEngine manages the "Think -> Act -> Observe" cycle.
type TurnEngine struct {
	ctxManager *ContextManager
	gateway    gateway.LLMGateway
	executor   IToolExecutor
	registry   ToolRegistry
	Hooks      TurnHooks
}

// TurnEngineOption defines a functional option for TurnEngine.
type TurnEngineOption func(*TurnEngine)

// WithHooks sets the TurnHooks for the engine.
func WithHooks(hooks TurnHooks) TurnEngineOption {
	return func(e *TurnEngine) {
		e.Hooks = hooks
	}
}

// NewTurnEngine creates a new TurnEngine with optional configuration.
func NewTurnEngine(gw gateway.LLMGateway, ex IToolExecutor, cm *ContextManager, reg ToolRegistry, opts ...TurnEngineOption) *TurnEngine {
	e := &TurnEngine{
		gateway:    gw,
		executor:   ex,
		ctxManager: cm,
		registry:   reg,
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Run executes the multi-turn orchestration loop.
func (e *TurnEngine) Run(ctx context.Context, startTime time.Time) error {
	for turn := 0; ; turn++ {
		// 1. Validation & Guards
		if err := e.validateTurn(ctx, turn); err != nil {
			return err
		}

		// 2. Execute the Atomic Turn
		state, err := e.executeTurn(ctx, turn, startTime)
		if err != nil {
			return err
		}

		// 3. Exit Condition
		if !state.HasToolCalls {
			break
		}
	}
	return nil
}

func (e *TurnEngine) validateTurn(ctx context.Context, turn int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if e.Hooks.OnTurnStart != nil {
		e.Hooks.OnTurnStart(turn)
	}

	_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
	if turn > maxTurns {
		return fmt.Errorf("%w: turn %d exceeds limit %d", ErrMaxTurnsReached, turn, maxTurns)
	}
	return nil
}

func (e *TurnEngine) executeTurn(ctx context.Context, turn int, startTime time.Time) (*TurnState, error) {
	// PHASE 1: THINK (Context + LLM Generation)
	apiContents, metadata, err := e.ctxManager.Prepare(ctx, turn)
	if err != nil {
		return nil, err
	}

	if e.Hooks.OnPrepare != nil {
		e.Hooks.OnPrepare(metadata)
	}

	respCh, finalize := e.gateway.Generate(ctx, apiContents, e.registry.GetDeclarations(), e.ctxManager.History.GetResolver())

	// UI streaming is now a decoupled concern
	if e.Hooks.OnStream != nil {
		e.Hooks.OnStream(ctx, respCh)
	} else {
		// Drain channel if no hook is provided
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return nil, err
	}

	if e.Hooks.OnResponse != nil {
		e.Hooks.OnResponse(respContent)
	}

	// PHASE 2: OBSERVE (Persistence)
	if err := e.ctxManager.History.AddContent(ctx, respContent); err != nil {
		return nil, fmt.Errorf("history error: %w", err)
	}

	// PHASE 3: ACT (Tool Execution)
	var toolResponse *types.Content
	if e.hasToolCalls(respContent) {
		toolResponse, err = e.handleToolExecution(ctx, respContent, turn, metrics)
		if err != nil {
			return nil, err
		}
	}

	state := &TurnState{
		HasToolCalls: toolResponse != nil,
		Metrics:      metrics,
		Tokens:       metadata.FinalTokenCount,
		CurrentTurns: metadata.FinalTurnCount,
	}

	if e.Hooks.OnComplete != nil {
		e.Hooks.OnComplete(state)
	}

	return state, nil
}

func (e *TurnEngine) handleToolExecution(ctx context.Context, respContent *types.Content, turn int, metrics *types.Metrics) (*types.Content, error) {
	toolStart := time.Now()
	_, maxToolTurns, _ := e.ctxManager.Strategy.GetLimits()

	toolResponse, err := e.executor.Execute(ctx, respContent, turn, maxToolTurns)
	if err != nil {
		return nil, err
	}

	if toolResponse != nil {
		if e.Hooks.OnToolResults != nil {
			e.Hooks.OnToolResults(toolResponse)
		}
		if err := e.ctxManager.History.AddContent(ctx, toolResponse); err != nil {
			return nil, fmt.Errorf("failed to persist tool results: %w", err)
		}
	}

	if metrics != nil {
		metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return toolResponse, nil
}

func (e *TurnEngine) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
