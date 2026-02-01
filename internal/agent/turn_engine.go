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

// Clock provides a way to get the current time, facilitating deterministic testing.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// TurnPhase represents the current stage of a single agent turn.
type TurnPhase string

const (
	PhaseRefining   TurnPhase = "Refining"
	PhaseInference  TurnPhase = "Inference"
	PhaseExecuting  TurnPhase = "Executing"
	PhasePersisting TurnPhase = "Persisting"
	PhaseRecovering TurnPhase = "Recovering"
	PhaseComplete   TurnPhase = "Complete"
)

// ProcessResult describes the outcome of a phase execution.
type ProcessResult struct {
	NextPhase TurnPhase
	Error     error
}

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
	LastError    error            `json:"-"`
	RetryCount   int              `json:"retry_count"`
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *types.Content, turn int, maxToolTurns int) (*types.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, turn *Turn) ProcessResult
}

// TurnProcessorFunc is an adapter to allow the use of ordinary functions as TurnProcessors.
type TurnProcessorFunc func(context.Context, *Turn) ProcessResult

// Process calls f(ctx, turn).
func (f TurnProcessorFunc) Process(ctx context.Context, turn *Turn) ProcessResult {
	return f(ctx, turn)
}

// TurnMiddleware wraps a TurnProcessor to inject cross-cutting concerns.
type TurnMiddleware func(TurnProcessor) TurnProcessor

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
	Clock        Clock

	// StreamHandler allows external handling of LLM response streams.
	StreamHandler func(context.Context, <-chan *types.Content)

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
	middleware []TurnMiddleware
	clock      Clock
}

// EngineOption allows configuring the TurnEngine.
type EngineOption func(*TurnEngine)

// WithMiddleware adds middleware to the TurnEngine.
func WithMiddleware(m ...TurnMiddleware) EngineOption {
	return func(e *TurnEngine) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithClock sets the clock for the TurnEngine.
func WithClock(c Clock) EngineOption {
	return func(e *TurnEngine) {
		e.clock = c
	}
}

// NewTurnEngine creates a new TurnEngine with a default pipeline.
func NewTurnEngine(gw gateway.LLMGateway, ex IToolExecutor, cm *ContextManager, reg ToolRegistry, bus events.EventBus, opts ...EngineOption) *TurnEngine {
	e := &TurnEngine{
		gateway:    gw,
		executor:   ex,
		ctxManager: cm,
		registry:   reg,
		events:     bus,
		processors: make(map[TurnPhase]TurnProcessor),
		clock:      realClock{},
	}

	e.processors[PhaseRefining] = &ContextRefiner{}
	e.processors[PhaseInference] = &InferenceStep{}
	e.processors[PhaseExecuting] = &ExecutionStep{}
	e.processors[PhasePersisting] = &PersistenceStep{}
	e.processors[PhaseRecovering] = &RecoveryStep{}

	for _, opt := range opts {
		opt(e)
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		e.middleware = append(e.middleware, WithEvents(e.events))
	}

	// Apply middleware in reverse order so the first one added is the outermost
	for i := len(e.middleware) - 1; i >= 0; i-- {
		m := e.middleware[i]
		for phase, p := range e.processors {
			e.processors[phase] = m(p)
		}
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
			Clock:      e.clock,
		}
		_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()

		if err := e.executeTurn(ctx, turn); err != nil {
			return err
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

		res := processor.Process(ctx, turn)
		if res.Error != nil {
			turn.State.LastError = res.Error
			// If we hit an error and are not already recovering, try to recover.
			if turn.State.Phase != PhaseRecovering {
				turn.State.Phase = PhaseRecovering
				continue
			}
			return res.Error
		}

		if res.NextPhase != "" {
			turn.State.Phase = res.NextPhase
		} else {
			turn.State.Phase = e.getNextPhase(turn.State.Phase, turn.State)
		}

		if turn.Stop && turn.State.Phase != PhaseComplete {
			break
		}
	}
	return nil
}

func (e *TurnEngine) getNextPhase(current TurnPhase, state *TurnState) TurnPhase {
	switch current {
	case PhaseRefining:
		return PhaseInference
	case PhaseInference:
		if state.HasToolCalls {
			return PhaseExecuting
		}
		return PhasePersisting
	case PhaseExecuting:
		return PhasePersisting
	case PhasePersisting:
		return PhaseComplete
	case PhaseRecovering:
		return PhaseInference
	default:
		return PhaseComplete
	}
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, turn *Turn) ProcessResult {
	apiContents, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		return ProcessResult{Error: err}
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.Metadata.APIContents = apiContents // Stash for InferenceStep

	return ProcessResult{}
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	apiContents := turn.State.Metadata.APIContents
	respCh, finalize := turn.Gateway.Generate(ctx, apiContents, turn.Registry.GetDeclarations(), turn.CtxManager.History.GetResolver())

	if turn.StreamHandler != nil {
		turn.StreamHandler(ctx, respCh)
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		return ProcessResult{Error: err}
	}
	if respContent == nil {
		return ProcessResult{Error: fmt.Errorf("api returned nil content")}
	}
	turn.State.Response = respContent
	turn.State.Metrics = metrics
	if metrics != nil {
		turn.State.Tokens = int(metrics.PromptTokens)
	}
	turn.State.HasToolCalls = p.hasToolCalls(respContent)

	return ProcessResult{}
}

func (p *InferenceStep) hasToolCalls(content *types.Content) bool {
	if content == nil {
		return false
	}
	for _, part := range content.Parts {
		if part.FunctionCall != nil {
			return true
		}
	}
	return false
}

// ExecutionStep executes tools if any.
type ExecutionStep struct{}

func (p *ExecutionStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	if !turn.State.HasToolCalls {
		return ProcessResult{}
	}

	toolStart := turn.Clock.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)
	if err != nil {
		return ProcessResult{Error: err}
	}

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = turn.Clock.Now().Sub(toolStart).Seconds()
	}
	return ProcessResult{}
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	if turn.State.Response != nil {
		if err := turn.CtxManager.History.AddContent(ctx, turn.State.Response); err != nil {
			return ProcessResult{Error: fmt.Errorf("history error: %w", err)}
		}
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.History.AddContent(ctx, turn.State.ToolResponse); err != nil {
			return ProcessResult{Error: fmt.Errorf("failed to persist tool results: %w", err)}
		}
	}

	return ProcessResult{}
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct{}

func (p *RecoveryStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	err := turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}
	}

	// Max retries
	if turn.State.RetryCount >= 3 {
		return ProcessResult{NextPhase: PhaseComplete, Error: fmt.Errorf("max retries reached: %w", err)}
	}

	if IsFatal(err) {
		return ProcessResult{NextPhase: PhaseComplete, Error: err}
	}

	if IsTransient(err) {
		turn.State.RetryCount++
		// Wait a bit before retrying
		select {
		case <-ctx.Done():
			return ProcessResult{Error: ctx.Err()}
		case <-time.After(time.Duration(turn.State.RetryCount) * 100 * time.Millisecond):
		}
		return ProcessResult{NextPhase: PhaseInference}
	}

	// Default to terminal for unknown errors to be safe
	return ProcessResult{NextPhase: PhaseComplete, Error: err}
}

// WithEvents returns a middleware that publishes events for various phases.
func WithEvents(bus events.EventBus) TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			// Setup for specific phases
			if turn.State.Phase == PhaseInference && bus != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *types.Content) {
					bus.Publish(events.ResponseStreamEvent{Context: ctx, Stream: stream})
				}
			}

			res := next.Process(ctx, turn)

			if bus == nil || res.Error != nil {
				return res
			}

			// Post-processing events
			switch turn.State.Phase {
			case PhaseRefining:
				maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
				bus.Publish(events.TurnStatusEvent{
					Status: events.TurnStatus{
						Timestamp:        turn.Clock.Now(),
						CurrentTurns:     turn.Index,
						MaxHistoryTurns:  maxHistTurns,
						Tokens:           turn.State.Tokens,
						MaxHistoryTokens: maxTokens,
						IsPostCall:       false,
						StartTime:        turn.StartTime,
					},
				})
			case PhasePersisting:
				if turn.State.Metrics != nil {
					bus.Publish(events.UsageMetricsEvent{
						Metrics:   turn.State.Metrics,
						StartTime: turn.StartTime,
					})
				}
				maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
				bus.Publish(events.TurnStatusEvent{
					Status: events.TurnStatus{
						Timestamp:        turn.Clock.Now(),
						CurrentTurns:     turn.Index,
						MaxHistoryTurns:  maxHistTurns,
						Tokens:           turn.State.Tokens,
						MaxHistoryTokens: maxTokens,
						Metrics:          turn.State.Metrics,
						IsPostCall:       true,
						StartTime:        turn.StartTime,
					},
				})
			}

			return res
		})
	}
}
