// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
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
	Stop      bool // Explicit signal to halt the turn
}

// RetryPolicy defines how the engine should handle errors and retries.
type RetryPolicy interface {
	ShouldRetry(err error, attempt int) (time.Duration, bool)
}

// DefaultRetryPolicy provides a standard retry implementation with linear backoff.
type DefaultRetryPolicy struct {
	MaxRetries int
	Backoff    time.Duration
}

func (p *DefaultRetryPolicy) ShouldRetry(err error, attempt int) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if IsFatal(err) {
		return 0, false
	}
	if IsTransient(err) {
		return time.Duration(attempt+1) * p.Backoff, true
	}
	return 0, false
}

// TurnHook allows intercepting lifecycle events of a turn.
type TurnHook interface {
	BeforeTurn(turn *Turn)
	AfterTurn(turn *Turn, err error)
	OnPhaseTransition(from, to TurnPhase, state *TurnState)
}

// TurnState carries data between the phases of a turn and tracks the current phase.
type TurnState struct {
	Phase        TurnPhase        `json:"phase"`
	HasToolCalls bool             `json:"has_tool_calls"`
	Metrics      *llm.Metrics     `json:"metrics,omitempty"`
	Tokens       int              `json:"tokens"`
	CurrentTurns int              `json:"current_turns"`
	Metadata     *ContextMetadata `json:"metadata,omitempty"`
	Response     *llm.Content     `json:"response,omitempty"`
	ToolResponse *llm.Content     `json:"tool_response,omitempty"`
	LastError    error            `json:"-"`
	RetryCount   int              `json:"retry_count"`
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
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
	StreamHandler func(context.Context, <-chan *llm.Content)

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type TurnEngine struct {
	ctxManager  *ContextManager
	gateway     gateway.LLMGateway
	executor    IToolExecutor
	registry    ToolRegistry
	events      events.EventBus
	processors  map[TurnPhase]TurnProcessor
	middleware  []TurnMiddleware
	hooks       []TurnHook
	retryPolicy RetryPolicy
	clock       Clock
}

// EngineOption allows configuring the TurnEngine.
type EngineOption func(*TurnEngine)

// WithMiddleware adds middleware to the TurnEngine.
func WithMiddleware(m ...TurnMiddleware) EngineOption {
	return func(e *TurnEngine) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithProcessor registers or overrides a processor for a specific phase.
func WithProcessor(phase TurnPhase, p TurnProcessor) EngineOption {
	return func(e *TurnEngine) {
		e.processors[phase] = p
	}
}

// WithHook adds a lifecycle hook to the TurnEngine.
func WithHook(h TurnHook) EngineOption {
	return func(e *TurnEngine) {
		e.hooks = append(e.hooks, h)
	}
}

// WithRetryPolicy sets the retry policy for the TurnEngine.
func WithRetryPolicy(p RetryPolicy) EngineOption {
	return func(e *TurnEngine) {
		e.retryPolicy = p
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
		gateway:     gw,
		executor:    ex,
		ctxManager:  cm,
		registry:    reg,
		events:      bus,
		processors:  make(map[TurnPhase]TurnProcessor),
		retryPolicy: &DefaultRetryPolicy{MaxRetries: 3, Backoff: 100 * time.Millisecond},
		clock:       realClock{},
	}

	// Register default processors
	e.processors[PhaseRefining] = &ContextRefiner{}
	e.processors[PhaseInference] = &InferenceStep{}
	e.processors[PhaseExecuting] = &ExecutionStep{}
	e.processors[PhasePersisting] = &PersistenceStep{}
	e.processors[PhaseRecovering] = &RecoveryStep{Policy: e.retryPolicy}

	for _, opt := range opts {
		opt(e)
	}

	// Ensure RecoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[PhaseRecovering].(*RecoveryStep); ok {
		rs.Policy = e.retryPolicy
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
	totalRetries := 0
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
		if i > maxTurns {
			return fmt.Errorf("%w: turn %d exceeds limit %d", llm.ErrMaxTurnsReached, i, maxTurns)
		}

		if e.events != nil {
			e.events.Publish(events.TurnStarted{Turn: i, MaxTurns: maxTurns})
		}

		turn := &Turn{
			Index:      i,
			StartTime:  startTime,
			State:      &TurnState{CurrentTurns: i, Phase: PhaseRefining, RetryCount: totalRetries},
			CtxManager: e.ctxManager,
			Gateway:    e.gateway,
			Executor:   e.executor,
			Registry:   e.registry,
			Events:     e.events,
			Clock:      e.clock,
		}
		_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()

		for _, h := range e.hooks {
			h.BeforeTurn(turn)
		}

		err := e.executeTurn(ctx, turn)

		for _, h := range e.hooks {
			h.AfterTurn(turn, err)
		}

		if err != nil {
			return err
		}

		totalRetries = turn.State.RetryCount

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
				for _, h := range e.hooks {
					h.OnPhaseTransition(turn.State.Phase, PhaseRecovering, turn.State)
				}
				turn.State.Phase = PhaseRecovering
				continue
			}
			return res.Error
		}

		next := res.NextPhase
		if next == "" {
			// This should ideally not happen in the new design, but we default to Complete for safety
			next = PhaseComplete
		}

		for _, h := range e.hooks {
			h.OnPhaseTransition(turn.State.Phase, next, turn.State)
		}

		turn.State.Phase = next
		if res.Stop {
			turn.Stop = true
		}

		if turn.Stop && turn.State.Phase != PhaseComplete {
			break
		}
	}
	return nil
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

	return ProcessResult{NextPhase: PhaseInference}
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

	if turn.State.HasToolCalls {
		return ProcessResult{NextPhase: PhaseExecuting}
	}
	return ProcessResult{NextPhase: PhasePersisting}
}

func (p *InferenceStep) hasToolCalls(content *llm.Content) bool {
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
		return ProcessResult{NextPhase: PhasePersisting}
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
	return ProcessResult{NextPhase: PhasePersisting}
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

	return ProcessResult{NextPhase: PhaseComplete}
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct {
	Policy RetryPolicy
}

func (p *RecoveryStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	err := turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}
	}

	delay, retry := p.Policy.ShouldRetry(err, turn.State.RetryCount)
	if !retry {
		if IsTransient(err) {
			return ProcessResult{NextPhase: PhaseComplete, Error: fmt.Errorf("max retries reached: %w", err)}
		}
		return ProcessResult{NextPhase: PhaseComplete, Error: err}
	}

	turn.State.RetryCount++

	if err := ctx.Err(); err != nil {
		return ProcessResult{Error: err}
	}

	select {
	case <-ctx.Done():
		return ProcessResult{Error: ctx.Err()}
	case <-time.After(delay):
	}

	return ProcessResult{NextPhase: PhaseInference}
}

// WithEvents returns a middleware that publishes events for various phases.
func WithEvents(bus events.EventBus) TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			// Setup for specific phases
			if turn.State.Phase == PhaseInference && bus != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
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
