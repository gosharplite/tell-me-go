// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/types"
)

// TurnPhase represents the current stage of a single agent turn.
type TurnPhase string

const (
	PhaseRefining   TurnPhase = "Refining"
	PhaseInference  TurnPhase = "Inference"
	PhaseExecuting  TurnPhase = "Executing"
	PhasePersisting TurnPhase = "Persisting"
	PhaseComplete   TurnPhase = "Complete"
)

// TurnState carries data between the phases of a turn and tracks the current phase.
type TurnState struct {
	Phase        TurnPhase        `json:"phase"`
	HasToolCalls bool             `json:"has_tool_calls"`
	Metrics      *types.Metrics   `json:"metrics,omitempty"`
	Tokens       int              `json:"tokens"`
	CurrentTurns int              `json:"current_turns"`
	Metadata     *ContextMetadata `json:"metadata,omitempty"`
	Response     *types.Content   `json:"response,omitempty"`
	ToolResponse *types.Content   `json:"tool_response,omitempty"`
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, turn *Turn) (TurnPhase, error)
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
	Events       events.EventBus
	MaxToolTurns int

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type TurnEngine struct {
	ctxManager *ContextManager
	gateway    gateway.LLMGateway
	executor   IToolExecutor
	registry   ToolRegistry
	events     events.EventBus
	processors map[TurnPhase]TurnProcessor
}

// NewTurnEngine creates a new TurnEngine with a default pipeline.
func NewTurnEngine(gw gateway.LLMGateway, ex IToolExecutor, cm *ContextManager, reg ToolRegistry, bus events.EventBus) *TurnEngine {
	e := &TurnEngine{
		gateway:    gw,
		executor:   ex,
		ctxManager: cm,
		registry:   reg,
		events:     bus,
		processors: make(map[TurnPhase]TurnProcessor),
	}

	e.processors[PhaseRefining] = &ContextRefiner{}
	e.processors[PhaseInference] = &InferenceStep{}
	e.processors[PhaseExecuting] = &ExecutionStep{}
	e.processors[PhasePersisting] = &PersistenceStep{}

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
			return fmt.Errorf("%w: turn %d exceeds limit %d", types.ErrMaxTurnsReached, i, maxTurns)
		}

		if e.events != nil {
			e.events.Publish(events.TurnStarted{Turn: i, MaxTurns: maxTurns})
		}

		turn := &Turn{
			Index:      i,
			StartTime:  startTime,
			State:      &TurnState{CurrentTurns: i, Phase: PhaseRefining},
			CtxManager: e.ctxManager,
			Gateway:    e.gateway,
			Executor:   e.executor,
			Registry:   e.registry,
			Events:     e.events,
		}
		_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()

		if err := e.executeTurn(ctx, turn); err != nil {
			return err
		}

		if e.events != nil {
			e.events.Publish(events.TurnStatusEvent{
				Status: e.getTurnStatus(turn.State.CurrentTurns, turn.State.Tokens, turn.State.Metrics, true, startTime),
			})
			if turn.State.Metrics != nil {
				e.events.Publish(events.UsageMetricsEvent{
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

func (e *TurnEngine) executeTurn(ctx context.Context, turn *Turn) error {
	for turn.State.Phase != PhaseComplete {
		processor, ok := e.processors[turn.State.Phase]
		if !ok {
			return fmt.Errorf("no processor for phase: %s", turn.State.Phase)
		}

		nextState, err := processor.Process(ctx, turn)
		if err != nil {
			return err
		}
		turn.State.Phase = nextState

		if turn.Stop && turn.State.Phase != PhaseComplete {
			// Allow premature exit if stop is requested, but usually
			// the processor should return PhaseComplete.
			break
		}
	}
	return nil
}

func (e *TurnEngine) getTurnStatus(currentTurns, tokens int, metrics *types.Metrics, isPost bool, startTime time.Time) events.TurnStatus {
	maxTokens, _, maxHistTurns := e.ctxManager.Strategy.GetLimits()
	return events.TurnStatus{
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

func (p *ContextRefiner) Process(ctx context.Context, turn *Turn) (TurnPhase, error) {
	apiContents, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		return PhaseComplete, err
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount

	if turn.Events != nil {
		maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
		turn.Events.Publish(events.TurnStatusEvent{
			Status: events.TurnStatus{
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
	return PhaseInference, nil
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *Turn) (TurnPhase, error) {
	apiContents := turn.State.Metadata.APIContents
	respCh, finalize := turn.Gateway.Generate(ctx, apiContents, turn.Registry.GetDeclarations(), turn.CtxManager.History.GetResolver())

	if turn.Events != nil {
		turn.Events.Publish(events.ResponseStreamEvent{Context: ctx, Stream: respCh})
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return PhaseComplete, err
	}
	turn.State.Response = respContent
	turn.State.Metrics = metrics
	turn.State.HasToolCalls = p.hasToolCalls(respContent)

	if turn.State.HasToolCalls {
		return PhaseExecuting, nil
	}
	return PhasePersisting, nil
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

func (p *ExecutionStep) Process(ctx context.Context, turn *Turn) (TurnPhase, error) {
	if !turn.State.HasToolCalls {
		return PhasePersisting, nil
	}

	toolStart := time.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)
	if err != nil {
		return PhaseComplete, err
	}

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = time.Since(toolStart).Seconds()
	}
	return PhasePersisting, nil
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, turn *Turn) (TurnPhase, error) {
	if err := turn.CtxManager.History.AddContent(ctx, turn.State.Response); err != nil {
		return PhaseComplete, fmt.Errorf("history error: %w", err)
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.History.AddContent(ctx, turn.State.ToolResponse); err != nil {
			return PhaseComplete, fmt.Errorf("failed to persist tool results: %w", err)
		}
	}

	return PhaseComplete, nil
}
