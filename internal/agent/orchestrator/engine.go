// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"

	"go.opentelemetry.io/otel"
)

// RuntimeConfig defines the runtime configuration for the Engine.
type RuntimeConfig struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
}

// TurnPhase represents the current stage of a single agent turn.
type TurnPhase string

const (
	PhaseGuard      TurnPhase = "Guard"
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
	Stop      bool // Explicit signal to halt the turn
	Recovery  bool // Explicit signal that we should enter recovery
}

// retryPolicy defines how the engine should handle errors and retries.
type retryPolicy interface {
	ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool)
}

// defaultRetryPolicy provides a standard retry implementation with exponential backoff and jitter.
type defaultRetryPolicy struct {
	MaxRetries       int
	Backoff          time.Duration
	RateLimitBackoff time.Duration
}

func (p *defaultRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if IsFatal(err) {
		return 0, false
	}
	if IsTransient(err) {
		base := p.Backoff

		// Use the severe backoff if we have been rate-limited at any point during this turn's
		// retry sequence, to avoid "flooding" the provider again.
		if hasSeenRateLimit {
			base = p.RateLimitBackoff
		}

		const maxDelay = 2 * time.Minute // Enforce an architectural ceiling

		delay := base

		// 1. Initial cap in case base > maxDelay
		if delay >= maxDelay {
			delay = maxDelay
		} else {
			// 2. Safely double the delay, breaking early to prevent int64 overflow
			for i := 0; i < attempt; i++ {
				delay *= 2
				if delay >= maxDelay {
					delay = maxDelay
					break
				}
			}
		}

		// 3. Apply Jitter
		finalDelay := time.Duration(c.Jitter(float64(delay)))

		return finalDelay, true
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
	Phase                TurnPhase         `json:"phase"`
	HasToolCalls         bool              `json:"has_tool_calls"`
	Metrics              *llm.Metrics      `json:"metrics,omitempty"`
	Tokens               int               `json:"tokens"`
	CurrentTurns         int               `json:"current_turns"`
	Metadata             *session.Metadata `json:"metadata,omitempty"`
	Response             *llm.Content      `json:"response,omitempty"`
	ToolResponse         *llm.Content      `json:"tool_response,omitempty"`
	LastError            error             `json:"-"`
	RetryCount           int               `json:"retry_count"`
	HasSeenRateLimit     bool              `json:"-"`
	ToolCallCount        map[string]int    `json:"-"`
	RecentResponseHashes []string          `json:"-"`
	PreparedHistory      []*llm.Content    `json:"-"`
	TaskCost             float64           `json:"task_cost"`
	ToolReasons          []string          `json:"-"`
}

// ToolExecutor defines the interface for tool execution.
type ToolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, turn *Turn) (ProcessResult, error)
}

// TurnProcessorFunc is an adapter to allow the use of ordinary functions as TurnProcessors.
type TurnProcessorFunc func(context.Context, *Turn) (ProcessResult, error)

// Process calls f(ctx, turn).
func (f TurnProcessorFunc) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	return f(ctx, turn)
}

// TurnMiddleware wraps a TurnProcessor to inject cross-cutting concerns.
type TurnMiddleware func(TurnProcessor) TurnProcessor

// Turn carries state and configuration for a single agent turn.
type Turn struct {
	Index        int
	StartTime    time.Time
	State        *TurnState
	CtxManager   *session.ContextManager
	Gateway      llm.LLMGateway
	Executor     ToolExecutor
	Registry     tools.Registry
	TokenCounter llm.TokenCounter
	Events       events.EventBus
	MaxToolTurns int
	Clock        clock.Clock
	CostTracker  domain_pricing.CostTracker
	ProviderName string
	Model        string
	Mode         string
	Logger       *slog.Logger

	// Results/Outputs
	Stop bool
}

// Engine manages the "Think -> Act -> Observe" cycle using a state machine.
type Engine struct {
	mu               sync.RWMutex
	ctxManager       *session.ContextManager
	gateway          llm.LLMGateway
	executor         ToolExecutor
	registry         tools.Registry
	tokenCounter     llm.TokenCounter
	events           events.EventBus
	processors       map[TurnPhase]TurnProcessor
	middleware       []TurnMiddleware
	hooks            []TurnHook
	retryPolicy      retryPolicy
	clock            clock.Clock
	sm               domain_security.Manager
	providerName     string
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	costTracker      domain_pricing.CostTracker
	logger           *slog.Logger
}

// WithEngineClock sets a custom clock implementation.
func WithEngineClock(c clock.Clock) EngineOption {
	return func(e *Engine) {
		e.clock = c
	}
}

// EngineOption allows configuring the Engine.
type EngineOption func(*Engine)

// WithEngineCostTracker sets the cost tracker for the engine.
func WithEngineCostTracker(tracker domain_pricing.CostTracker) EngineOption {
	return func(e *Engine) {
		e.costTracker = tracker
	}
}

// WithEngineConfig sets the security and usage configuration for the engine.
func WithEngineConfig(sm domain_security.Manager, providerName, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) EngineOption {
	return func(e *Engine) {
		e.sm = sm
		e.providerName = providerName
		e.model = model
		e.mode = mode
		e.pricingOverrides = pricingOverrides
	}
}

// ApplyOptions applies new options to the engine.
func (e *Engine) ApplyOptions(opts ...EngineOption) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, opt := range opts {
		opt(e)
	}
}

// Reconfigure propagates configuration changes to the engine.
func (e *Engine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.CostTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providerName = cfg.ProviderName
	e.model = cfg.Model
	e.mode = cfg.Mode
	e.pricingOverrides = cfg.PricingOverrides
	e.costTracker = tracker
}

// NewEngine creates a new Engine with a default pipeline.
func NewEngine(gw llm.LLMGateway, ex ToolExecutor, cm *session.ContextManager, reg tools.Registry, bus events.EventBus, counter llm.TokenCounter, opts ...EngineOption) *Engine {
	e := &Engine{
		gateway:      gw,
		executor:     ex,
		ctxManager:   cm,
		registry:     reg,
		tokenCounter: counter,
		events:       bus,
		processors:   make(map[TurnPhase]TurnProcessor),
		retryPolicy:  &defaultRetryPolicy{MaxRetries: 6, Backoff: 2 * time.Second, RateLimitBackoff: 5 * time.Second},
		clock:        clock.RealClock{},
	}

	// Register default processors
	e.processors[PhaseGuard] = &guardStep{}
	e.processors[PhaseRefining] = &contextRefiner{}
	e.processors[PhaseInference] = &inferenceStep{}
	e.processors[PhaseExecuting] = &executionStep{}
	e.processors[PhasePersisting] = &persistenceStep{}
	e.processors[PhaseRecovering] = &recoveryStep{Policy: e.retryPolicy}

	for _, opt := range opts {
		opt(e)
	}

	// Ensure recoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[PhaseRecovering].(*recoveryStep); ok {
		rs.Policy = e.retryPolicy
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		e.middleware = append(e.middleware,
			e.WithMetrics(),
			e.WithStatusReporter(),
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
func (e *Engine) Run(ctx context.Context, startTime time.Time) error {
	sessionToolCallCount := make(map[string]int)
	turn := e.createTurn(0, startTime)
	turn.State.ToolCallCount = sessionToolCallCount

	for turn.State.Phase != PhaseComplete {
		err := e.executeTurn(ctx, turn)
		if err != nil {
			return err
		}

		if e.shouldStopRunning(turn) {
			break
		}

		// Prepare for next turn
		e.prepareNextTurn(turn)
	}
	return nil
}

func (e *Engine) prepareNextTurn(turn *Turn) {
	turn.Index++
	turn.State.CurrentTurns = turn.Index
	turn.State.Phase = PhaseGuard
	turn.State.RetryCount = 0
	turn.State.Response = nil
	turn.State.ToolResponse = nil
	turn.State.HasToolCalls = false
	turn.State.ToolReasons = nil
}

func (e *Engine) createTurn(index int, startTime time.Time) *Turn {
	e.mu.RLock()
	tracker := e.costTracker
	providerName := e.providerName
	model := e.model
	mode := e.mode
	counter := e.tokenCounter
	logger := e.getLogger()
	e.mu.RUnlock()

	turn := &Turn{
		Index:        index,
		StartTime:    startTime,
		State:        &TurnState{CurrentTurns: index, Phase: PhaseGuard, RetryCount: 0},
		CtxManager:   e.ctxManager,
		Gateway:      e.gateway,
		Executor:     e.executor,
		Registry:     e.registry,
		TokenCounter: counter,
		Events:       e.events,
		Clock:        e.clock,
		CostTracker:  tracker,
		ProviderName: providerName,
		Model:        model,
		Mode:         mode,
		Logger:       logger,
	}
	turn.MaxToolTurns = e.ctxManager.GetLimits().MaxToolTurns
	return turn
}

func (e *Engine) notifyBeforeTurn(turn *Turn) {
	for _, h := range e.hooks {
		h.BeforeTurn(turn)
	}
}

func (e *Engine) notifyAfterTurn(turn *Turn, err error) {
	for _, h := range e.hooks {
		h.AfterTurn(turn, err)
	}
}

func (e *Engine) shouldStopRunning(turn *Turn) bool {
	return !turn.State.HasToolCalls || turn.Stop
}

func (e *Engine) executeTurn(parentCtx context.Context, turn *Turn) error {
	ctx, span := otel.Tracer("agent").Start(parentCtx, "agent.turn")
	defer span.End()

	trace := telemetry.NewTurnTrace()
	ctxWithTrace := telemetry.ContextWithTrace(ctx, trace)

	e.notifyBeforeTurn(turn)

	err := e.runPhaseLoop(ctxWithTrace, turn)

	e.finalizeTurnTrace(trace, err)
	if err := events.SafePublish(ctx, e.events, events.TraceEvent{Trace: trace}); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.getLogger().Error("event_publish_failed",
				slog.String("event_type", "TraceEvent"),
				slog.Any("error", err))
		}
	}

	e.notifyAfterTurn(turn, err)
	return err
}

func (e *Engine) runPhaseLoop(ctx context.Context, turn *Turn) error {
	for turn.State.Phase != PhaseComplete {
		res, err := e.executePhase(ctx, turn)
		if err != nil && turn.State.Phase == PhaseComplete {
			e.emergencySave(turn)
			return err
		}
		if res.Stop {
			turn.Stop = true
			return err
		}
	}
	return nil
}

func (e *Engine) finalizeTurnTrace(trace *telemetry.TurnTrace, err error) {
	trace.EndTime = e.clock.Now()
	trace.FinalStatus = "success"
	if err != nil {
		trace.FinalStatus = "error"
	}
}

func (e *Engine) emergencySave(turn *Turn) {
	if turn.State.Response != nil && len(turn.State.Response.Parts) > 0 {
		if p, ok := e.processors[PhasePersisting]; ok {
			saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = p.Process(saveCtx, turn)
		}
	}
}

func (e *Engine) executePhase(ctx context.Context, turn *Turn) (ProcessResult, error) {
	processor, ok := e.processors[turn.State.Phase]
	if !ok {
		return ProcessResult{}, NewAgentError(ErrLogic, fmt.Sprintf("no processor for phase: %s", turn.State.Phase), nil)
	}

	res, err := processor.Process(ctx, turn)
	if err != nil {
		turn.State.LastError = err
	}

	next := e.determineNextPhase(turn.State.Phase, res, err)
	e.notifyTransition(turn.State.Phase, next, turn.State)
	turn.State.Phase = next

	return res, err
}

func (e *Engine) determineNextPhase(current TurnPhase, res ProcessResult, err error) TurnPhase {
	if (err != nil || res.Recovery) && current != PhaseRecovering {
		return PhaseRecovering
	}
	if res.NextPhase != "" {
		return res.NextPhase
	}
	return PhaseComplete
}

func (e *Engine) notifyTransition(from, to TurnPhase, state *TurnState) {
	for _, h := range e.hooks {
		h.OnPhaseTransition(from, to, state)
	}
}

// guardStep validates the turn against limits before proceeding.
type guardStep struct{}

func (p *guardStep) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	maxTurns := turn.CtxManager.GetLimits().MaxToolTurns
	if turn.Index > maxTurns {
		return ProcessResult{}, NewAgentError(llm.ErrTerminal, fmt.Sprintf("turn %d exceeds limit %d", turn.Index, maxTurns), llm.ErrMaxTurnsReached)
	}

	evt := events.TurnStarted{Turn: turn.Index, MaxTurns: maxTurns}
	if err := events.SafePublish(ctx, turn.Events, evt); err != nil {
		if errors.Is(err, events.ErrBusNotInitialized) {
			return ProcessResult{NextPhase: PhaseRefining}, nil
		}
		turn.getLogger().Error("event_publish_failed",
			slog.String("event_type", string(evt.Type())),
			slog.Any("error", err))
		return ProcessResult{}, err
	}
	return ProcessResult{NextPhase: PhaseRefining}, nil
}

// contextRefiner prepares the context for the LLM call.
type contextRefiner struct{}

func (p *contextRefiner) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	history, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		category := llm.ErrTerminal
		if IsTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "context preparation failed", err)
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.PreparedHistory = history

	return ProcessResult{NextPhase: PhaseInference}, nil
}

// inferenceStep calls the LLM.
type inferenceStep struct{}

func (p *inferenceStep) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	start := turn.Clock.Now()
	respContent, metrics, err := p.invokeModel(ctx, turn)
	inferenceDuration := turn.Clock.Now().Sub(start)

	if trace := telemetry.TraceFromContext(ctx); trace != nil {
		trace.InferenceDuration = inferenceDuration
	}

	if respContent != nil {
		p.updateState(turn, respContent, metrics)
	}

	if err != nil {
		category := llm.ErrTerminal
		if IsTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "inference failed", err)
	}

	return p.routeBasedOnContent(respContent), nil
}

func (p *inferenceStep) invokeModel(ctx context.Context, turn *Turn) (respContent *llm.Content, metrics *llm.Metrics, err error) {
	_ = events.SafePublish(ctx, turn.Events, events.InferenceStartedEvent{Model: turn.Model})

	defer func() {
		safeContent := respContent
		if safeContent == nil {
			safeContent = &llm.Content{Role: "model"}
		}
		// Detach context to ensure the UI ALWAYS receives the stop signal even on timeout
		stopCtx := context.WithoutCancel(ctx)
		if err := events.SafePublish(stopCtx, turn.Events, events.ResponseEvent{Content: safeContent}); err != nil {
			turn.getLogger().ErrorContext(stopCtx, "Failed to publish ResponseEvent; UI spinner may hang",
				slog.Any("error", err))
		}
	}()

	var activeToolkits []string
	if turn.CtxManager != nil && turn.CtxManager.SessionProvider != nil {
		activeToolkits = turn.CtxManager.SessionProvider.GetInfo().ActiveToolkits
	}

	var activeTools []*tools.ToolDeclaration
	if len(activeToolkits) > 0 {
		activeTools = turn.Registry.GetDeclarationsByToolkits(activeToolkits)
	} else {
		activeTools = turn.Registry.GetCoreDeclarations()
	}

	respContent, metrics, err = turn.Gateway.Generate(ctx, turn.State.PreparedHistory, activeTools, turn.CtxManager.History.GetResolver())
	if err == nil && respContent == nil {
		return nil, nil, NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return respContent, metrics, err
}

func (p *inferenceStep) updateState(turn *Turn, content *llm.Content, metrics *llm.Metrics) {
	turn.State.Response = content
	turn.State.Metrics = metrics
	if metrics != nil {
		metrics.Model = turn.Model
		metrics.Provider = turn.ProviderName
		turn.State.Tokens = int(metrics.PromptTokens)
	}
	turn.State.HasToolCalls = p.hasToolCalls(content)
	if turn.State.HasToolCalls {
		// Preallocate capacity based on the number of parts in the response
		turn.State.ToolReasons = make([]string, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				if reason, ok := part.FunctionCall.Args["reason"].(string); ok && reason != "" {
					turn.State.ToolReasons = append(turn.State.ToolReasons, reason)
				}
			}
		}
	}
}

func (p *inferenceStep) routeBasedOnContent(content *llm.Content) ProcessResult {
	if p.hasToolCalls(content) {
		return ProcessResult{NextPhase: PhaseExecuting}
	}
	return ProcessResult{NextPhase: PhasePersisting}
}

func (p *inferenceStep) hasToolCalls(content *llm.Content) bool {
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

// executionStep executes tools if any.
type executionStep struct{}

func (p *executionStep) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	if !turn.State.HasToolCalls {
		return ProcessResult{NextPhase: PhasePersisting}, nil
	}

	var names []string
	if turn.State.Response != nil {
		names = make([]string, 0, len(turn.State.Response.Parts))
		for _, part := range turn.State.Response.Parts {
			if part.FunctionCall != nil {
				names = append(names, part.FunctionCall.Name)
			}
		}
	}
	_ = events.SafePublish(ctx, turn.Events, events.ToolExecutionStartedEvent{ToolNames: names})

	toolStart := turn.Clock.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
		p.validatePayloadLimits(ctx, turn)
	}

	if err != nil {
		return ProcessResult{}, p.handleToolExecutionError(err)
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = turn.Clock.Now().Sub(toolStart).Seconds()
		if trace := telemetry.TraceFromContext(ctx); trace != nil {
			turn.State.Metrics.CumulativeToolDuration = trace.CumulativeToolDuration().Seconds()
		}
	}
	return ProcessResult{NextPhase: PhasePersisting}, nil
}

func (p *executionStep) handleToolExecutionError(err error) error {
	category := llm.ErrTerminal
	if IsTransient(err) {
		category = llm.ErrTransient
	}
	return NewAgentError(category, "tool execution failed", err)
}

// persistenceStep saves the response and tool results to history.
type persistenceStep struct{}

func (p *persistenceStep) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	if turn.State.Response != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.Response); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "history error", err)
		}
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.ToolResponse); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "failed to persist tool results", err)
		}
	}

	return ProcessResult{NextPhase: PhaseComplete}, nil
}

// recoveryStep handles errors by deciding whether to retry or fail.
type recoveryStep struct {
	Policy retryPolicy
}

func (p *recoveryStep) Process(ctx context.Context, turn *Turn) (ProcessResult, error) {
	err := turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}, nil
	}

	// State mutation is handled by the workflow engine (caller)
	isRateLimit := errors.Is(err, llm.ErrRateLimit)
	if isRateLimit {
		turn.State.HasSeenRateLimit = true
	}

	delay, retry := p.Policy.ShouldRetry(turn.Clock, err, turn.State.RetryCount, turn.State.HasSeenRateLimit)
	if !retry {
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, turn, delay)
}

func (p *recoveryStep) handleFailure(err error) (ProcessResult, error) {
	if IsTransient(err) {
		return ProcessResult{NextPhase: PhaseComplete}, fmt.Errorf("max retries reached: %w", err)
	}
	return ProcessResult{NextPhase: PhaseComplete}, err
}

func (p *recoveryStep) attemptRetry(ctx context.Context, turn *Turn, delay time.Duration) (ProcessResult, error) {
	turn.State.RetryCount++

	// Log retry to application logs (Technical debugging only)
	turn.getLogger().Debug("retrying_after_transient_error",
		slog.Any("error", turn.State.LastError),
		slog.Duration("delay", delay),
		slog.Int("attempt", turn.State.RetryCount))

	// Publish retry notification to the UI/EventBus
	msg := fmt.Sprintf("Transient error: %v. Retrying in %v (Attempt %d)...",
		turn.State.LastError, delay.Round(time.Millisecond), turn.State.RetryCount)
	evt := events.SystemMessageEvent{
		Message: msg,
		Level:   "warn",
	}
	if err := events.SafePublish(ctx, turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			turn.getLogger().Error("event_publish_failed",
				slog.String("event_type", string(evt.Type())),
				slog.Any("error", err))
			return ProcessResult{}, err
		}
	}

	// Publish RetryWaitingEvent to show the spinner during the backoff delay
	_ = events.SafePublish(ctx, turn.Events, events.RetryWaitingEvent{Duration: delay})

	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	select {
	case <-ctx.Done():
		return ProcessResult{}, ctx.Err()
	case <-turn.Clock.After(delay):
	}

	return ProcessResult{NextPhase: PhaseRefining}, nil
}

func (p *executionStep) validatePayloadLimits(ctx context.Context, turn *Turn) {
	if turn.State.ToolResponse == nil || turn.CtxManager == nil || turn.CtxManager.Strategy == nil {
		return
	}

	limits := turn.CtxManager.GetLimits()
	if limits.MaxHistoryTokens <= 0 {
		return
	}

	toolTokens := turn.TokenCounter.Count([]*llm.Content{turn.State.ToolResponse})
	isTooLarge, instruction := p.checkTokenBudget(turn, toolTokens, limits)

	if isTooLarge {
		p.handleOversizedPayload(ctx, turn, toolTokens, instruction)
	}
}

func (p *executionStep) checkTokenBudget(turn *Turn, toolTokens int, limits events.Limits) (bool, string) {
	// We use the remaining buffer, accounting for the 10% system reservation
	maxAllowed := int(float64(limits.MaxHistoryTokens) * 0.90)

	// Cap individual tool response size to 50% of total limit just in case,
	// AND ensure it doesn't push the total over the cliff.
	if toolTokens > int(float64(limits.MaxHistoryTokens)*0.50) {
		return true, "The individual tool output is too massive. You MUST use precise boundaries (e.g., 'tail_lines', 'max_lines', 'limit', or 'grep'). Summarizing history will not fix this."
	} else if turn.State.Tokens+toolTokens > maxAllowed {
		return true, "The total conversation context is nearly exhausted. Please call 'summarize_history' first to free up space, then run the tool again."
	}

	return false, ""
}

func (p *executionStep) handleOversizedPayload(ctx context.Context, turn *Turn, toolTokens int, instruction string) {
	// Delegate mutation to the utility with context-aware instruction
	TruncateOversizedResponse(turn.State.ToolResponse, toolTokens, instruction)

	evt := events.SystemMessageEvent{
		Message: fmt.Sprintf("Tool output truncated (~%d tokens) to prevent exceeding safety limit.", toolTokens),
		Level:   "error",
	}
	if err := events.SafePublish(ctx, turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			turn.getLogger().Error("event_publish_failed",
				slog.String("event_type", string(evt.Type())),
				slog.Any("error", err))
		}
	}
}

func (e *Engine) getLogger() *slog.Logger {
	if e.logger != nil {
		return e.logger
	}
	return slog.Default()
}

func (t *Turn) getLogger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

// WithEngineLogger sets the logger for the engine.
func WithEngineLogger(l *slog.Logger) EngineOption {
	return func(e *Engine) {
		e.logger = l
	}
}
