// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

// turnPhase represents the current stage of a single agent turn.
type turnPhase string

const (
	phaseGuard      turnPhase = "Guard"
	phaseRefining   turnPhase = "Refining"
	phaseInference  turnPhase = "Inference"
	phaseExecuting  turnPhase = "Executing"
	phasePersisting turnPhase = "Persisting"
	phaseRecovering turnPhase = "Recovering"
	phaseComplete   turnPhase = "Complete"
)

// processResult describes the outcome of a phase execution.
type processResult struct {
	NextPhase turnPhase
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
	if isFatal(err) {
		return 0, false
	}
	if isTransient(err) {
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

// turnHook allows intercepting lifecycle events of a turn.
type turnHook interface {
	BeforeTurn(turn *turn)
	AfterTurn(turn *turn, err error)
	OnPhaseTransition(from, to turnPhase, state *turnState)
}

// turnState carries data between the phases of a turn and tracks the current phase.
type turnState struct {
	Phase                turnPhase         `json:"phase"`
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

// toolExecutor defines the interface for tool execution.
type toolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

// turnProcessor defines a single stage in the TurnEngine pipeline.
type turnProcessor interface {
	process(ctx context.Context, turn *turn) (processResult, error)
}

// turnProcessorFunc is an adapter to allow the use of ordinary functions as turnProcessors.
type turnProcessorFunc func(context.Context, *turn) (processResult, error)

// process calls f(ctx, turn).
func (f turnProcessorFunc) process(ctx context.Context, turn *turn) (processResult, error) {
	return f(ctx, turn)
}

// turnMiddleware wraps a turnProcessor to inject cross-cutting concerns.
type turnMiddleware func(turnProcessor) turnProcessor

// turn carries state and configuration for a single agent turn.
type turn struct {
	Index        int
	StartTime    time.Time
	State        *turnState
	CtxManager   *session.ContextManager
	Gateway      llm.LLMGateway
	executor     toolExecutor
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

// turnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type turnEngine struct {
	mu               sync.RWMutex
	ctxManager       *session.ContextManager
	gateway          llm.LLMGateway
	executor         toolExecutor
	registry         tools.Registry
	tokenCounter     llm.TokenCounter
	events           events.EventBus
	processors       map[turnPhase]turnProcessor
	middleware       []turnMiddleware
	hooks            []turnHook
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

// withEngineClock sets a custom clock implementation.
func withEngineClock(c clock.Clock) engineOption {
	return func(e *turnEngine) {
		e.clock = c
	}
}

// engineOption allows configuring the turnEngine.
type engineOption func(*turnEngine)

// withEngineCostTracker sets the cost tracker for the engine.
func withEngineCostTracker(tracker domain_pricing.CostTracker) engineOption {
	return func(e *turnEngine) {
		e.costTracker = tracker
	}
}

// withEngineConfig sets the security and usage configuration for the engine.
func withEngineConfig(sm domain_security.Manager, providerName, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) engineOption {
	return func(e *turnEngine) {
		e.sm = sm
		e.providerName = providerName
		e.model = model
		e.mode = mode
		e.pricingOverrides = pricingOverrides
	}
}

// ApplyOptions applies new options to the engine.
func (e *turnEngine) ApplyOptions(opts ...engineOption) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, opt := range opts {
		opt(e)
	}
}

// Reconfigure propagates configuration changes to the engine.
func (e *turnEngine) Reconfigure(cfg runtimeConfig, tracker domain_pricing.CostTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.providerName = cfg.ProviderName
	e.model = cfg.Model
	e.mode = cfg.Mode
	e.pricingOverrides = cfg.PricingOverrides
	e.costTracker = tracker
}

// newTurnEngine creates a new turnEngine with a default pipeline.
func newTurnEngine(gw llm.LLMGateway, ex toolExecutor, cm *session.ContextManager, reg tools.Registry, bus events.EventBus, counter llm.TokenCounter, opts ...engineOption) *turnEngine {
	e := &turnEngine{
		gateway:      gw,
		executor:     ex,
		ctxManager:   cm,
		registry:     reg,
		tokenCounter: counter,
		events:       bus,
		processors:   make(map[turnPhase]turnProcessor),
		retryPolicy:  &defaultRetryPolicy{MaxRetries: 6, Backoff: 2 * time.Second, RateLimitBackoff: 5 * time.Second},
		clock:        clock.RealClock{},
	}

	// Register default processors
	e.processors[phaseGuard] = &guardStep{}
	e.processors[phaseRefining] = &contextRefiner{}
	e.processors[phaseInference] = &inferenceStep{}
	e.processors[phaseExecuting] = &executionStep{}
	e.processors[phasePersisting] = &persistenceStep{}
	e.processors[phaseRecovering] = &recoveryStep{Policy: e.retryPolicy}

	for _, opt := range opts {
		opt(e)
	}

	// Ensure recoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[phaseRecovering].(*recoveryStep); ok {
		rs.Policy = e.retryPolicy
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		e.middleware = append(e.middleware,
			e.WithMetrics(),
			e.WithStatusReporter(),
			withLoopDetector(),
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
func (e *turnEngine) Run(ctx context.Context, startTime time.Time) error {
	sessionToolCallCount := make(map[string]int)
	turn := e.createTurn(0, startTime)
	turn.State.ToolCallCount = sessionToolCallCount

	for turn.State.Phase != phaseComplete {
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

func (e *turnEngine) prepareNextTurn(turn *turn) {
	turn.Index++
	turn.State.CurrentTurns = turn.Index
	turn.State.Phase = phaseGuard
	turn.State.RetryCount = 0
	turn.State.Response = nil
	turn.State.ToolResponse = nil
	turn.State.HasToolCalls = false
	turn.State.ToolReasons = nil
}

func (e *turnEngine) createTurn(index int, startTime time.Time) *turn {
	e.mu.RLock()
	tracker := e.costTracker
	providerName := e.providerName
	model := e.model
	mode := e.mode
	counter := e.tokenCounter
	logger := e.getLogger()
	e.mu.RUnlock()

	turn := &turn{
		Index:        index,
		StartTime:    startTime,
		State:        &turnState{CurrentTurns: index, Phase: phaseGuard, RetryCount: 0},
		CtxManager:   e.ctxManager,
		Gateway:      e.gateway,
		executor:     e.executor,
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

func (e *turnEngine) notifyBeforeTurn(turn *turn) {
	for _, h := range e.hooks {
		h.BeforeTurn(turn)
	}
}

func (e *turnEngine) notifyAfterTurn(turn *turn, err error) {
	for _, h := range e.hooks {
		h.AfterTurn(turn, err)
	}
}

func (e *turnEngine) shouldStopRunning(turn *turn) bool {
	return !turn.State.HasToolCalls || turn.Stop
}

func (e *turnEngine) executeTurn(parentCtx context.Context, turn *turn) error {
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

func (e *turnEngine) runPhaseLoop(ctx context.Context, turn *turn) error {
	for turn.State.Phase != phaseComplete {
		res, err := e.executePhase(ctx, turn)
		if err != nil && turn.State.Phase == phaseComplete {
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

func (e *turnEngine) finalizeTurnTrace(trace *telemetry.TurnTrace, err error) {
	trace.EndTime = e.clock.Now()
	trace.FinalStatus = "success"
	if err != nil {
		trace.FinalStatus = "error"
	}
}

func (e *turnEngine) emergencySave(turn *turn) {
	if turn.State.Response != nil && len(turn.State.Response.Parts) > 0 {
		if p, ok := e.processors[phasePersisting]; ok {
			saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = p.process(saveCtx, turn)
		}
	}
}

func (e *turnEngine) executePhase(ctx context.Context, turn *turn) (processResult, error) {
	processor, ok := e.processors[turn.State.Phase]
	if !ok {
		return processResult{}, newAgentError(errLogic, fmt.Sprintf("no processor for phase: %s", turn.State.Phase), nil)
	}

	res, err := processor.process(ctx, turn)
	if err != nil {
		turn.State.LastError = err
	}

	next := e.determineNextPhase(turn.State.Phase, res, err)
	e.notifyTransition(turn.State.Phase, next, turn.State)
	turn.State.Phase = next

	return res, err
}

func (e *turnEngine) determineNextPhase(current turnPhase, res processResult, err error) turnPhase {
	if (err != nil || res.Recovery) && current != phaseRecovering {
		return phaseRecovering
	}
	if res.NextPhase != "" {
		return res.NextPhase
	}
	return phaseComplete
}

func (e *turnEngine) notifyTransition(from, to turnPhase, state *turnState) {
	for _, h := range e.hooks {
		h.OnPhaseTransition(from, to, state)
	}
}

// guardStep validates the turn against limits before proceeding.
type guardStep struct{}

func (p *guardStep) process(ctx context.Context, turn *turn) (processResult, error) {
	if err := ctx.Err(); err != nil {
		return processResult{}, err
	}

	maxTurns := turn.CtxManager.GetLimits().MaxToolTurns
	if turn.Index > maxTurns {
		return processResult{}, newAgentError(llm.ErrTerminal, fmt.Sprintf("turn %d exceeds limit %d", turn.Index, maxTurns), llm.ErrMaxTurnsReached)
	}

	evt := events.TurnStarted{Turn: turn.Index, MaxTurns: maxTurns}
	if err := events.SafePublish(ctx, turn.Events, evt); err != nil {
		if errors.Is(err, events.ErrBusNotInitialized) {
			return processResult{NextPhase: phaseRefining}, nil
		}
		turn.getLogger().Error("event_publish_failed",
			slog.String("event_type", string(evt.Type())),
			slog.Any("error", err))
		return processResult{}, err
	}
	return processResult{NextPhase: phaseRefining}, nil
}

// contextRefiner prepares the context for the LLM call.
type contextRefiner struct{}

func (p *contextRefiner) process(ctx context.Context, turn *turn) (processResult, error) {
	history, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		category := llm.ErrTerminal
		if isTransient(err) {
			category = llm.ErrTransient
		}
		return processResult{}, newAgentError(category, "context preparation failed", err)
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.PreparedHistory = history

	return processResult{NextPhase: phaseInference}, nil
}

// inferenceStep calls the LLM.
type inferenceStep struct{}

func (p *inferenceStep) process(ctx context.Context, turn *turn) (processResult, error) {
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
		if isTransient(err) {
			category = llm.ErrTransient
		}
		return processResult{}, newAgentError(category, "inference failed", err)
	}

	return p.routeBasedOnContent(respContent), nil
}

func (p *inferenceStep) invokeModel(ctx context.Context, turn *turn) (respContent *llm.Content, metrics *llm.Metrics, err error) {
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
		return nil, nil, newAgentError(errLogic, "api returned nil content", nil)
	}
	return respContent, metrics, err
}

func (p *inferenceStep) updateState(turn *turn, content *llm.Content, metrics *llm.Metrics) {
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

func (p *inferenceStep) routeBasedOnContent(content *llm.Content) processResult {
	if p.hasToolCalls(content) {
		return processResult{NextPhase: phaseExecuting}
	}
	return processResult{NextPhase: phasePersisting}
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

func (p *executionStep) process(ctx context.Context, turn *turn) (processResult, error) {
	if !turn.State.HasToolCalls {
		return processResult{NextPhase: phasePersisting}, nil
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

	toolResponse, err := turn.executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
		p.injectCircuitBreakerWarning(ctx, turn, toolResponse)
		p.validatePayloadLimits(ctx, turn)
	}

	if err != nil {
		return processResult{}, p.handleToolExecutionError(err)
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = turn.Clock.Now().Sub(toolStart).Seconds()
		if trace := telemetry.TraceFromContext(ctx); trace != nil {
			turn.State.Metrics.CumulativeToolDuration = trace.CumulativeToolDuration().Seconds()
		}
	}
	return processResult{NextPhase: phasePersisting}, nil
}

func (p *executionStep) handleToolExecutionError(err error) error {
	category := llm.ErrTerminal
	if isTransient(err) {
		category = llm.ErrTransient
	}
	return newAgentError(category, "tool execution failed", err)
}

func (p *executionStep) injectCircuitBreakerWarning(ctx context.Context, turn *turn, toolResponse *llm.Content) {
	if toolResponse == nil {
		return
	}
	// Check if any tool triggered the circuit breaker
	for _, part := range toolResponse.Parts {
		if part.FunctionResponse != nil {
			if res, ok := part.FunctionResponse.Response["result"].(string); ok {
				if strings.Contains(res, "temporarily disabled") && strings.Contains(res, "multiple consecutive failures") {
					// Append the safety warning directly to the tool response
					// so it gets persisted in the correct chronological order during the persistenceStep.
					toolResponse.Parts = append(toolResponse.Parts, &llm.Part{
						Text: "SYSTEM WARNING: A tool has been temporarily disabled due to multiple consecutive failures. Please continue the task without attempting to use that specific tool again.",
					})
					break
				}
			}
		}
	}
}

// persistenceStep saves the response and tool results to history.
type persistenceStep struct{}

func (p *persistenceStep) process(ctx context.Context, turn *turn) (processResult, error) {
	if turn.State.Response != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.Response); err != nil {
			category := llm.ErrTerminal
			if isTransient(err) {
				category = llm.ErrTransient
			}
			return processResult{}, newAgentError(category, "history error", err)
		}
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.ToolResponse); err != nil {
			category := llm.ErrTerminal
			if isTransient(err) {
				category = llm.ErrTransient
			}
			return processResult{}, newAgentError(category, "failed to persist tool results", err)
		}
	}

	return processResult{NextPhase: phaseComplete}, nil
}

// recoveryStep handles errors by deciding whether to retry or fail.
type recoveryStep struct {
	Policy retryPolicy
}

func (p *recoveryStep) process(ctx context.Context, turn *turn) (processResult, error) {
	err := turn.State.LastError
	if err == nil {
		return processResult{NextPhase: phaseComplete}, nil
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

func (p *recoveryStep) handleFailure(err error) (processResult, error) {
	if isTransient(err) {
		return processResult{NextPhase: phaseComplete}, fmt.Errorf("max retries reached: %w", err)
	}
	return processResult{NextPhase: phaseComplete}, err
}

func (p *recoveryStep) attemptRetry(ctx context.Context, turn *turn, delay time.Duration) (processResult, error) {
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
			return processResult{}, err
		}
	}

	// Publish RetryWaitingEvent to show the spinner during the backoff delay
	_ = events.SafePublish(ctx, turn.Events, events.RetryWaitingEvent{Duration: delay})

	if err := ctx.Err(); err != nil {
		return processResult{}, err
	}

	select {
	case <-ctx.Done():
		return processResult{}, ctx.Err()
	case <-turn.Clock.After(delay):
	}

	return processResult{NextPhase: phaseRefining}, nil
}

func (p *executionStep) validatePayloadLimits(ctx context.Context, turn *turn) {
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

func (p *executionStep) checkTokenBudget(turn *turn, toolTokens int, limits events.Limits) (bool, string) {
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

func (p *executionStep) handleOversizedPayload(ctx context.Context, turn *turn, toolTokens int, instruction string) {
	// Delegate mutation to the utility with context-aware instruction
	truncateOversizedResponse(turn.State.ToolResponse, toolTokens, instruction)

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

func (e *turnEngine) getLogger() *slog.Logger {
	if e.logger != nil {
		return e.logger
	}
	return slog.Default()
}

func (t *turn) getLogger() *slog.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

// withEngineLogger sets the logger for the engine.
func withEngineLogger(l *slog.Logger) engineOption {
	return func(e *turnEngine) {
		e.logger = l
	}
}
