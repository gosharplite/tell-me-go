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

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, turn *Turn) error
}

// Turn carries state and configuration for a single agent turn.
type Turn struct {
	Index        int
	StartTime    time.Time
	State        *TurnState
	CtxManager   *ContextManager
	Gateway      gateway.LLMGateway
	Executor     IToolExecutor
	Registry     ToolRegistry
	Events       EventBus
	MaxToolTurns int

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a pipeline of processors.
type TurnEngine struct {
	ctxManager *ContextManager
	gateway    gateway.LLMGateway
	executor   IToolExecutor
	registry   ToolRegistry
	events     EventBus
	processors []TurnProcessor
}

// NewTurnEngine creates a new TurnEngine with a default pipeline.
func NewTurnEngine(gw gateway.LLMGateway, ex IToolExecutor, cm *ContextManager, reg ToolRegistry, events EventBus) *TurnEngine {
	e := &TurnEngine{
		gateway:    gw,
		executor:   ex,
		ctxManager: cm,
		registry:   reg,
		events:     events,
	}

	e.processors = []TurnProcessor{
		&ContextRefiner{},
		&InferenceStep{},
		&ExecutionStep{},
		&PersistenceStep{},
	}

	return e
}

// Run executes the multi-turn orchestration loop.
func (e *TurnEngine) Run(ctx context.Context, startTime time.Time) error {
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
		if i > maxTurns {
			return fmt.Errorf("%w: turn %d exceeds limit %d", ErrMaxTurnsReached, i, maxTurns)
		}

		if e.events != nil {
			e.events.Publish(TurnStarted{Turn: i, MaxTurns: maxTurns})
		}

		turn := &Turn{
			Index:      i,
			StartTime:  startTime,
			State:      &TurnState{CurrentTurns: i, Phase: PhasePreparing},
			CtxManager: e.ctxManager,
			Gateway:    e.gateway,
			Executor:   e.executor,
			Registry:   e.registry,
			Events:     e.events,
		}
		_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()

		for _, p := range e.processors {
			if err := p.Process(ctx, turn); err != nil {
				return err
			}
			if turn.Stop {
				break
			}
		}

		if e.events != nil {
			e.events.Publish(TurnStatusEvent{
				Status: e.getTurnStatus(turn.State.CurrentTurns, turn.State.Tokens, turn.State.Metrics, true, startTime),
			})
			if turn.State.Metrics != nil {
				e.events.Publish(UsageMetricsEvent{
					Metrics:   turn.State.Metrics,
					StartTime: startTime,
				})
			}
		}

		if !turn.State.HasToolCalls || turn.Stop {
			break
		}
	}
	return nil
}

func (e *TurnEngine) getTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool, startTime time.Time) TurnStatus {
	maxTokens, _, maxHistTurns := e.ctxManager.Strategy.GetLimits()
	return TurnStatus{
		Timestamp:        time.Now(),
		CurrentTurns:     currentTurns,
		MaxHistoryTurns:  maxHistTurns,
		Tokens:           tokens,
		MaxHistoryTokens: maxTokens,
		Metrics:          metrics,
		IsPostCall:       isPost,
		StartTime:        startTime,
	}
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, turn *Turn) error {
	apiContents, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		return err
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.Phase = PhaseThinking

	if turn.Events != nil {
		maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
		turn.Events.Publish(TurnStatusEvent{
			Status: TurnStatus{
				Timestamp:        time.Now(),
				CurrentTurns:     turn.Index,
				MaxHistoryTurns:  maxHistTurns,
				Tokens:           turn.State.Tokens,
				MaxHistoryTokens: maxTokens,
				IsPostCall:       false,
				StartTime:        turn.StartTime,
			},
		})
	}

	turn.State.Metadata.APIContents = apiContents // Stash for InferenceStep
	return nil
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *Turn) error {
	apiContents := turn.State.Metadata.APIContents
	respCh, finalize := turn.Gateway.Generate(ctx, apiContents, turn.Registry.GetDeclarations(), turn.CtxManager.History.GetResolver())

	if turn.Events != nil {
		turn.Events.Publish(ResponseStreamEvent{Context: ctx, Stream: respCh})
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return err
	}
	turn.State.Response = respContent
	turn.State.Metrics = metrics
	turn.State.HasToolCalls = p.hasToolCalls(respContent)
	turn.State.Phase = PhaseObserving

	return nil
}

func (p *InferenceStep) hasToolCalls(content *types.Content) bool {
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// ExecutionStep executes tools if any.
type ExecutionStep struct{}

func (p *ExecutionStep) Process(ctx context.Context, turn *Turn) error {
	if !turn.State.HasToolCalls {
		return nil
	}

	turn.State.Phase = PhaseExecuting
	toolStart := time.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)
	if err != nil {
		return err
	}

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return nil
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, turn *Turn) error {
	if err := turn.CtxManager.History.AddContent(ctx, turn.State.Response); err != nil {
		return fmt.Errorf("history error: %w", err)
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.History.AddContent(ctx, turn.State.ToolResponse); err != nil {
			return fmt.Errorf("failed to persist tool results: %w", err)
		}
	}

	turn.State.Phase = PhaseComplete
	return nil
}

