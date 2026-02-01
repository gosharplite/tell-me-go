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

// TurnPhase represents the current stage of a single agent turn.
type TurnPhase string

const (
	PhasePreparing TurnPhase = "Preparing"
	PhaseThinking  TurnPhase = "Thinking"
	PhaseObserving TurnPhase = "Observing"
	PhaseExecuting TurnPhase = "Executing"
	PhaseComplete  TurnPhase = "Complete"
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

// TurnState carries data between the phases of a turn and tracks the current phase.
type TurnState struct {
	Phase        TurnPhase
	HasToolCalls bool
	Metrics      *types.Metrics
	Tokens       int
	CurrentTurns int
	Metadata     *ContextMetadata
	Response     *types.Content
	ToolResponse *types.Content
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error)
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using an explicit state machine.
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
		if err := e.validateTurn(ctx, turn); err != nil {
			return err
		}

		state, err := e.executeTurn(ctx, turn, startTime)
		if err != nil {
			return err
		}

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
	state := &TurnState{
		CurrentTurns: turn,
		Phase:        PhasePreparing,
	}

	// 1. Preparation & Thinking
	if err := e.transitionToThinking(ctx, state); err != nil {
		return nil, err
	}

	// 2. Observation (Persistence of model response)
	if err := e.transitionToObserving(ctx, state); err != nil {
		return nil, err
	}

	// 3. Execution (Tool Action)
	if state.HasToolCalls {
		if err := e.transitionToExecuting(ctx, state); err != nil {
			return nil, err
		}
	}

	// 4. Finalization
	state.Phase = PhaseComplete
	if e.Hooks.OnComplete != nil {
		e.Hooks.OnComplete(state)
	}

	return state, nil
}

func (e *TurnEngine) transitionToThinking(ctx context.Context, state *TurnState) error {
	apiContents, metadata, err := e.ctxManager.Prepare(ctx, state.CurrentTurns)
	if err != nil {
		return err
	}
	state.Metadata = metadata
	state.Tokens = metadata.FinalTokenCount

	if e.Hooks.OnPrepare != nil {
		e.Hooks.OnPrepare(metadata)
	}

	state.Phase = PhaseThinking
	respCh, finalize := e.gateway.Generate(ctx, apiContents, e.registry.GetDeclarations(), e.ctxManager.History.GetResolver())

	if e.Hooks.OnStream != nil {
		e.Hooks.OnStream(ctx, respCh)
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return err
	}
	state.Response = respContent
	state.Metrics = metrics
	state.HasToolCalls = e.hasToolCalls(respContent)

	if e.Hooks.OnResponse != nil {
		e.Hooks.OnResponse(respContent)
	}
	return nil
}

func (e *TurnEngine) transitionToObserving(ctx context.Context, state *TurnState) error {
	state.Phase = PhaseObserving
	if err := e.ctxManager.History.AddContent(ctx, state.Response); err != nil {
		return fmt.Errorf("history error: %w", err)
	}
	return nil
}

func (e *TurnEngine) transitionToExecuting(ctx context.Context, state *TurnState) error {
	state.Phase = PhaseExecuting
	toolStart := time.Now()
	_, maxToolTurns, _ := e.ctxManager.Strategy.GetLimits()

	toolResponse, err := e.executor.Execute(ctx, state.Response, state.CurrentTurns, maxToolTurns)
	if err != nil {
		return err
	}

	if toolResponse != nil {
		state.ToolResponse = toolResponse
		if e.Hooks.OnToolResults != nil {
			e.Hooks.OnToolResults(toolResponse)
		}
		if err := e.ctxManager.History.AddContent(ctx, toolResponse); err != nil {
			return fmt.Errorf("failed to persist tool results: %w", err)
		}
	}

	if state.Metrics != nil {
		state.Metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return nil
}

func (e *TurnEngine) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}
