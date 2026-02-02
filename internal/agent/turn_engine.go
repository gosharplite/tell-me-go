// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/events"
	"github.com/gosharplite/tell-me-go/internal/agent/gateway"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
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
	Recovery  bool // Explicit signal that we should enter recovery
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
	Phase         TurnPhase        `json:"phase"`
	HasToolCalls  bool             `json:"has_tool_calls"`
	Metrics       *llm.Metrics     `json:"metrics,omitempty"`
	Tokens        int              `json:"tokens"`
	CurrentTurns  int              `json:"current_turns"`
	Metadata      *ContextMetadata `json:"metadata,omitempty"`
	Response      *llm.Content     `json:"response,omitempty"`
	ToolResponse  *llm.Content     `json:"tool_response,omitempty"`
	LastError     error            `json:"-"`
	RetryCount    int              `json:"retry_count"`
	ToolCallCount map[string]int   `json:"-"`
	LastResponse  string           `json:"-"`
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
	CostTracker  *framework.SessionCostTracker

	// StreamHandler allows external handling of LLM response streams.
	StreamHandler func(context.Context, <-chan *llm.Content)

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type TurnEngine struct {
	ctxManager       *ContextManager
	gateway          gateway.LLMGateway
	executor         IToolExecutor
	registry         ToolRegistry
	events           events.EventBus
	processors       map[TurnPhase]TurnProcessor
	middleware       []TurnMiddleware
	hooks            []TurnHook
	retryPolicy      RetryPolicy
	clock            Clock
	sm               *security.SecurityManager
	logFile          string
	model            string
	pricingOverrides map[string]llm.ModelPricing
	costTracker      *framework.SessionCostTracker
	HardBudgetLimit  float64
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

// WithHardBudget sets a maximum session budget in USD.
func WithHardBudget(limit float64) EngineOption {
	return func(e *TurnEngine) {
		e.HardBudgetLimit = limit
	}
}

// WithConfig sets the security and usage configuration for the engine.
func WithConfig(sm *security.SecurityManager, logFile, model string, pricingOverrides map[string]llm.ModelPricing) EngineOption {
	return func(e *TurnEngine) {
		e.sm = sm
		e.logFile = logFile
		e.model = model
		e.pricingOverrides = pricingOverrides

		// Initialize cost tracker if we have the necessary info
		if sm != nil && logFile != "" && model != "" {
			pricing := framework.GetPricing(context.Background(), sm, filepath.Dir(logFile))
			for k, v := range pricingOverrides {
				pricing.Models[k] = v
			}
			p := framework.GetModelPricing(model, pricing)
			e.costTracker = framework.NewSessionCostTracker(sm, logFile, p, pricing)
			go e.costTracker.Warmup()
		}
	}
}

// Reconfigure applies new options to the engine.
func (e *TurnEngine) Reconfigure(opts ...EngineOption) {
	for _, opt := range opts {
		opt(e)
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
		// Subscribe the cost tracker to metrics events via delegation to allow reconfiguration
		// without leaking subscribers or handling unsubscription.
		e.events.Subscribe(func(ev events.Event) {
			if um, ok := ev.(events.UsageMetricsEvent); ok {
				if e.costTracker != nil && um.Metrics != nil {
					e.costTracker.Accumulate(*um.Metrics)
				}
			}
		})

		e.middleware = append(e.middleware,
			WithStreaming(e.events),
			WithStatusReporter(e.events),
			WithMetrics(e.events),
			WithLoopDetector(),
		)
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
	var lastState *TurnState
	for i := 0; ; i++ {
		if err := e.checkLimits(ctx, i); err != nil {
			return err
		}

		turn := e.createTurn(i, startTime, totalRetries)
		if lastState != nil {
			turn.State.ToolCallCount = lastState.ToolCallCount
			turn.State.LastResponse = lastState.LastResponse
		}
		if turn.State.ToolCallCount == nil {
			turn.State.ToolCallCount = make(map[string]int)
		}

		e.notifyBeforeTurn(turn)

		err := e.executeTurn(ctx, turn)
		e.notifyAfterTurn(turn, err)

		if err != nil {
			return err
		}

		totalRetries = turn.State.RetryCount
		lastState = turn.State
		if e.shouldStopRunning(turn) {
			break
		}
	}
	return nil
}

func (e *TurnEngine) checkLimits(ctx context.Context, turnIndex int) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if e.costTracker != nil && e.HardBudgetLimit > 0 {
		if cost := e.costTracker.GetTotalCost(ctx); cost > e.HardBudgetLimit {
			return fmt.Errorf("%w: current cost $%.4f exceeds budget $%.4f", llm.ErrBudgetExceeded, cost, e.HardBudgetLimit)
		}
	}

	_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
	if turnIndex > maxTurns {
		return fmt.Errorf("%w: turn %d exceeds limit %d", llm.ErrMaxTurnsReached, turnIndex, maxTurns)
	}

	if e.events != nil {
		e.events.Publish(events.TurnStarted{Turn: turnIndex, MaxTurns: maxTurns})
	}
	return nil
}

func (e *TurnEngine) createTurn(index int, startTime time.Time, totalRetries int) *Turn {
	turn := &Turn{
		Index:      index,
		StartTime:  startTime,
		State:      &TurnState{CurrentTurns: index, Phase: PhaseRefining, RetryCount: totalRetries},
		CtxManager: e.ctxManager,
		Gateway:    e.gateway,
		Executor:   e.executor,
		Registry:   e.registry,
		Events:     e.events,
		Clock:      e.clock,
		CostTracker: e.costTracker,
	}
	_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()
	return turn
}

func (e *TurnEngine) notifyBeforeTurn(turn *Turn) {
	for _, h := range e.hooks {
		h.BeforeTurn(turn)
	}
}

func (e *TurnEngine) notifyAfterTurn(turn *Turn, err error) {
	for _, h := range e.hooks {
		h.AfterTurn(turn, err)
	}
}

func (e *TurnEngine) shouldStopRunning(turn *Turn) bool {
	return !turn.State.HasToolCalls || turn.Stop
}

func (e *TurnEngine) executeTurn(ctx context.Context, turn *Turn) error {
	for turn.State.Phase != PhaseComplete {
		res, err := e.executePhase(ctx, turn)
		if err != nil {
			return err
		}
		if e.shouldBreak(turn, res) {
			if res.Error != nil {
				return res.Error
			}
			break
		}
	}
	return nil
}

func (e *TurnEngine) executePhase(ctx context.Context, turn *Turn) (ProcessResult, error) {
	processor, ok := e.processors[turn.State.Phase]
	if !ok {
		return ProcessResult{}, fmt.Errorf("no processor for phase: %s", turn.State.Phase)
	}

	res := processor.Process(ctx, turn)
	if res.Error != nil {
		turn.State.LastError = res.Error
	}

	next := e.determineNextPhase(turn.State.Phase, res)
	e.notifyTransition(turn.State.Phase, next, turn.State)
	turn.State.Phase = next

	if res.Error != nil && next == PhaseComplete {
		return res, res.Error
	}
	return res, nil
}

func (e *TurnEngine) shouldBreak(turn *Turn, res ProcessResult) bool {
	if res.Stop {
		turn.Stop = true
	}
	return turn.Stop && turn.State.Phase != PhaseComplete
}

func (e *TurnEngine) determineNextPhase(current TurnPhase, res ProcessResult) TurnPhase {
	if (res.Error != nil || res.Recovery) && current != PhaseRecovering {
		return PhaseRecovering
	}
	if res.NextPhase != "" {
		return res.NextPhase
	}
	return PhaseComplete
}

func (e *TurnEngine) notifyTransition(from, to TurnPhase, state *TurnState) {
	for _, h := range e.hooks {
		h.OnPhaseTransition(from, to, state)
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

	return ProcessResult{NextPhase: PhaseInference}
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *Turn) ProcessResult {
	respContent, metrics, err := p.invokeModel(ctx, turn)
	if err != nil {
		return ProcessResult{Error: err}
	}

	p.updateState(turn, respContent, metrics)

	return p.routeBasedOnContent(respContent)
}

func (p *InferenceStep) invokeModel(ctx context.Context, turn *Turn) (*llm.Content, *llm.Metrics, error) {
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
		return nil, nil, err
	}
	if respContent == nil {
		return nil, nil, fmt.Errorf("api returned nil content")
	}
	return respContent, metrics, nil
}

func (p *InferenceStep) updateState(turn *Turn, content *llm.Content, metrics *llm.Metrics) {
	turn.State.Response = content
	turn.State.Metrics = metrics
	if metrics != nil {
		turn.State.Tokens = int(metrics.PromptTokens)
	}
	turn.State.HasToolCalls = p.hasToolCalls(content)
}

func (p *InferenceStep) routeBasedOnContent(content *llm.Content) ProcessResult {
	if p.hasToolCalls(content) {
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
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, turn, delay)
}

func (p *RecoveryStep) handleFailure(err error) ProcessResult {
	if IsTransient(err) {
		return ProcessResult{NextPhase: PhaseComplete, Error: fmt.Errorf("max retries reached: %w", err)}
	}
	return ProcessResult{NextPhase: PhaseComplete, Error: err}
}

func (p *RecoveryStep) attemptRetry(ctx context.Context, turn *Turn, delay time.Duration) ProcessResult {
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

// WithStreaming returns a middleware that injects a stream handler into the turn.
func WithStreaming(bus events.EventBus) TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			if turn.State.Phase == PhaseInference && bus != nil {
				turn.StreamHandler = func(ctx context.Context, stream <-chan *llm.Content) {
					bus.Publish(events.ResponseStreamEvent{Context: ctx, Stream: stream})
				}
			}
			return next.Process(ctx, turn)
		})
	}
}

// WithStatusReporter returns a middleware that publishes turn status events.
func WithStatusReporter(bus events.EventBus) TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)
			if bus == nil || res.Error != nil {
				return res
			}

			if turn.State.Phase == PhaseRefining || turn.State.Phase == PhasePersisting {
				maxTokens, _, maxHistTurns := turn.CtxManager.Strategy.GetLimits()
				threshold := turn.CtxManager.Strategy.GetTieredThreshold()

				var cost float64
				if turn.State.Phase == PhasePersisting && turn.CostTracker != nil {
					cost = turn.CostTracker.GetTotalCost(ctx)
				}

				bus.Publish(events.TurnStatusEvent{
					Status: events.TurnStatus{
						Timestamp:        turn.Clock.Now(),
						CurrentTurns:     turn.Index,
						MaxHistoryTurns:  maxHistTurns,
						Tokens:           turn.State.Tokens,
						MaxHistoryTokens: maxTokens,
						TieredThreshold:  threshold,
						Metrics:          turn.State.Metrics,
						IsPostCall:       turn.State.Phase == PhasePersisting,
						StartTime:        turn.StartTime,
						SessionCost:      cost,
					},
				})
			}
			return res
		})
	}
}

// WithMetrics returns a middleware that publishes usage metrics.
func WithMetrics(bus events.EventBus) TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)
			if bus != nil && turn.State.Phase == PhasePersisting && turn.State.Metrics != nil {
				bus.Publish(events.UsageMetricsEvent{
					Metrics:   turn.State.Metrics,
					StartTime: turn.StartTime,
				})
			}
			return res
		})
	}
}

// WithLoopDetector returns a middleware that detects and breaks infinite tool loops.
func WithLoopDetector() TurnMiddleware {
	return func(next TurnProcessor) TurnProcessor {
		return TurnProcessorFunc(func(ctx context.Context, turn *Turn) ProcessResult {
			res := next.Process(ctx, turn)

			if turn.State.Phase == PhaseInference && res.Error == nil && turn.State.Response != nil {
				// 1. Text loop detection
				currentText := ""
				for _, p := range turn.State.Response.Parts {
					currentText += p.Text
				}
				if currentText != "" && currentText == turn.State.LastResponse {
					return ProcessResult{
						Stop:  true,
						Error: fmt.Errorf("infinite loop detected: model is repeating the exact same text response"),
					}
				}
				turn.State.LastResponse = currentText

				// 2. Tool call loop detection
				for _, p := range turn.State.Response.Parts {
					if p.FunctionCall != nil {
						args, _ := json.Marshal(p.FunctionCall.Args)
						key := p.FunctionCall.Name + ":" + string(args)
						turn.State.ToolCallCount[key]++
						if turn.State.ToolCallCount[key] > 5 {
							return ProcessResult{
								Stop:  true,
								Error: fmt.Errorf("infinite loop detected: tool '%s' called with same arguments %d times", p.FunctionCall.Name, turn.State.ToolCallCount[key]),
							}
						}
					}
				}
			}

			return res
		})
	}
}
