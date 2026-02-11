// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Clock provides a way to get the current time and handle delays, facilitating deterministic testing.
type Clock interface {
	Now() time.Time
	After(d time.Duration) <-chan time.Time
}

type realClock struct{}

func (realClock) Now() time.Time                         { return time.Now() }
func (realClock) After(d time.Duration) <-chan time.Time { return time.After(d) }

// turnPhase represents the current stage of a single agent turn.
type turnPhase string

const (
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

// turnHook allows intercepting lifecycle events of a turn.
type turnHook interface {
	BeforeTurn(turn *turn)
	AfterTurn(turn *turn, err error)
	OnPhaseTransition(from, to turnPhase, state *turnState)
}

// turnState carries data between the phases of a turn and tracks the current phase.
type turnState struct {
	Phase                turnPhase               `json:"phase"`
	HasToolCalls         bool                    `json:"has_tool_calls"`
	Metrics              *llm.Metrics            `json:"metrics,omitempty"`
	Tokens               int                     `json:"tokens"`
	CurrentTurns         int                     `json:"current_turns"`
	Metadata             *orchestration.Metadata `json:"metadata,omitempty"`
	Response             *llm.Content            `json:"response,omitempty"`
	ToolResponse         *llm.Content            `json:"tool_response,omitempty"`
	LastError            error                   `json:"-"`
	RetryCount           int                     `json:"retry_count"`
	ToolCallCount        map[string]int          `json:"-"`
	RecentResponseHashes []string                `json:"-"`
	PreparedHistory      []*llm.Content          `json:"-"`
	TaskCost             float64                 `json:"task_cost"`
}

// IToolExecutor defines the interface for tool execution.
type IToolExecutor interface {
	Execute(ctx context.Context, respContent *llm.Content, turn int, maxToolTurns int) (*llm.Content, error)
}

// turnProcessor defines a single stage in the TurnEngine pipeline.
type turnProcessor interface {
	Process(ctx context.Context, turn *turn) processResult
}

// turnProcessorFunc is an adapter to allow the use of ordinary functions as turnProcessors.
type turnProcessorFunc func(context.Context, *turn) processResult

// Process calls f(ctx, turn).
func (f turnProcessorFunc) Process(ctx context.Context, turn *turn) processResult {
	return f(ctx, turn)
}

// turnMiddleware wraps a turnProcessor to inject cross-cutting concerns.
type turnMiddleware func(turnProcessor) turnProcessor

// turn carries state and configuration for a single agent turn.
type turn struct {
	Index        int
	StartTime    time.Time
	State        *turnState
	CtxManager   *orchestration.ContextManager
	Gateway      llm.LLMGateway
	Executor     IToolExecutor
	Registry     tools.IToolRegistry
	Events       events.EventBus
	MaxToolTurns int
	Clock        Clock
	CostTracker  domain_pricing.ICostTracker
	Model        string

	// StreamHandler allows external handling of LLM response streams.
	StreamHandler func(context.Context, <-chan *llm.Content)

	// Results/Outputs
	Stop bool
}

// TurnEngine manages the "Think -> Act -> Observe" cycle using a state machine.
type TurnEngine struct {
	mu               sync.RWMutex
	ctxManager       *orchestration.ContextManager
	gateway          llm.LLMGateway
	executor         IToolExecutor
	registry         tools.IToolRegistry
	events           events.EventBus
	processors       map[turnPhase]turnProcessor
	middleware       []turnMiddleware
	hooks            []turnHook
	retryPolicy      RetryPolicy
	clock            Clock
	sm               domain_security.ISecurityManager
	model            string
	pricingOverrides map[string]domain_pricing.ModelPricing
	costTracker      domain_pricing.ICostTracker
	HardBudgetLimit  float64 // Internal guardrail. Default 0.0 = Disabled.
}

// EngineOption allows configuring the TurnEngine.
type EngineOption func(*TurnEngine)

// WithMiddleware adds middleware to the TurnEngine.
func WithMiddleware(m ...turnMiddleware) EngineOption {
	return func(e *TurnEngine) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithProcessor registers or overrides a processor for a specific phase.
func WithProcessor(phase turnPhase, p turnProcessor) EngineOption {
	return func(e *TurnEngine) {
		e.processors[phase] = p
	}
}

// WithHook adds a lifecycle hook to the TurnEngine.
func WithHook(h turnHook) EngineOption {
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
// Feature is intended for internal/API use only to maintain a clean UI.
func WithHardBudget(limit float64) EngineOption {
	return func(e *TurnEngine) {
		e.HardBudgetLimit = limit
	}
}

// WithCostTracker sets the cost tracker for the engine.
func WithCostTracker(tracker domain_pricing.ICostTracker) EngineOption {
	return func(e *TurnEngine) {
		e.costTracker = tracker
	}
}

// WithConfig sets the security and usage configuration for the engine.
func WithConfig(sm domain_security.ISecurityManager, model string, pricingOverrides map[string]domain_pricing.ModelPricing) EngineOption {
	return func(e *TurnEngine) {
		e.sm = sm
		e.model = model
		e.pricingOverrides = pricingOverrides
	}
}

// ApplyOptions applies new options to the engine.
func (e *TurnEngine) ApplyOptions(opts ...EngineOption) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, opt := range opts {
		opt(e)
	}
}

// Reconfigure propagates configuration changes to the engine.
func (e *TurnEngine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.ICostTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.model = cfg.Model
	e.pricingOverrides = cfg.PricingOverrides
	e.HardBudgetLimit = cfg.HardBudgetLimit
	e.costTracker = tracker
}

// NewTurnEngine creates a new TurnEngine with a default pipeline.
func NewTurnEngine(gw llm.LLMGateway, ex IToolExecutor, cm *orchestration.ContextManager, reg tools.IToolRegistry, bus events.EventBus, opts ...EngineOption) *TurnEngine {
	e := &TurnEngine{
		gateway:     gw,
		executor:    ex,
		ctxManager:  cm,
		registry:    reg,
		events:      bus,
		processors:  make(map[turnPhase]turnProcessor),
		retryPolicy: &DefaultRetryPolicy{MaxRetries: 3, Backoff: 100 * time.Millisecond},
		clock:       realClock{},
	}

	// Register default processors
	e.processors[phaseRefining] = &ContextRefiner{}
	e.processors[phaseInference] = &InferenceStep{}
	e.processors[phaseExecuting] = &ExecutionStep{}
	e.processors[phasePersisting] = &PersistenceStep{}
	e.processors[phaseRecovering] = &RecoveryStep{Policy: e.retryPolicy}

	for _, opt := range opts {
		opt(e)
	}

	// Ensure RecoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[phaseRecovering].(*RecoveryStep); ok {
		rs.Policy = e.retryPolicy
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		e.middleware = append(e.middleware,
			e.WithStreaming(),
			e.WithStatusReporter(),
			e.WithMetrics(),
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
	var lastState *turnState
	sessionToolCallCount := make(map[string]int)
	for i := 0; ; i++ {
		if err := e.checkLimits(ctx, i); err != nil {
			return err
		}

		trace := telemetry.NewTurnTrace()
		ctxWithTrace := telemetry.ContextWithTrace(ctx, trace)

		turn := e.createTurn(i, startTime)
		if lastState != nil {
			// Only carry over response hashes to detect text/turn repetition loops
			turn.State.RecentResponseHashes = append([]string(nil), lastState.RecentResponseHashes...)
			turn.State.TaskCost = lastState.TaskCost
		}
		// Tool calls are tracked at the session level to detect loops spanning multiple turn boundaries.
		turn.State.ToolCallCount = sessionToolCallCount

		e.notifyBeforeTurn(turn)

		err := e.executeTurn(ctxWithTrace, turn)

		trace.EndTime = time.Now()
		trace.FinalStatus = "success"
		if err != nil {
			trace.FinalStatus = "error"
		}

		if e.events != nil {
			e.events.Publish(events.TraceEvent{Trace: trace})
		}

		e.notifyAfterTurn(turn, err)

		if err != nil {
			return err
		}

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

	// Deterministic Budget Guardrail (API/Internal only)
	e.mu.RLock()
	limit := e.HardBudgetLimit
	tracker := e.costTracker
	e.mu.RUnlock()

	if limit > 0 && tracker != nil {
		if cost := tracker.GetTotalCost(ctx); cost >= limit {
			return NewAgentError(llm.ErrTerminal, fmt.Sprintf("current session cost $%.4f exceeds internal limit $%.4f", cost, limit), llm.ErrBudgetExceeded)
		}
	}

	_, maxTurns, _ := e.ctxManager.Strategy.GetLimits()
	if turnIndex > maxTurns {
		return NewAgentError(llm.ErrTerminal, fmt.Sprintf("turn %d exceeds limit %d", turnIndex, maxTurns), llm.ErrMaxTurnsReached)
	}

	if e.events != nil {
		e.events.Publish(events.TurnStarted{Turn: turnIndex, MaxTurns: maxTurns})
	}
	return nil
}

func (e *TurnEngine) createTurn(index int, startTime time.Time) *turn {
	e.mu.RLock()
	tracker := e.costTracker
	model := e.model
	e.mu.RUnlock()

	turn := &turn{
		Index:       index,
		StartTime:   startTime,
		State:       &turnState{CurrentTurns: index, Phase: phaseRefining, RetryCount: 0},
		CtxManager:  e.ctxManager,
		Gateway:     e.gateway,
		Executor:    e.executor,
		Registry:    e.registry,
		Events:      e.events,
		Clock:       e.clock,
		CostTracker: tracker,
		Model:       model,
	}
	_, turn.MaxToolTurns, _ = e.ctxManager.Strategy.GetLimits()
	return turn
}

func (e *TurnEngine) notifyBeforeTurn(turn *turn) {
	for _, h := range e.hooks {
		h.BeforeTurn(turn)
	}
}

func (e *TurnEngine) notifyAfterTurn(turn *turn, err error) {
	for _, h := range e.hooks {
		h.AfterTurn(turn, err)
	}
}

func (e *TurnEngine) shouldStopRunning(turn *turn) bool {
	return !turn.State.HasToolCalls || turn.Stop
}

func (e *TurnEngine) executeTurn(ctx context.Context, turn *turn) error {
	for turn.State.Phase != phaseComplete {
		res, err := e.executePhase(ctx, turn)
		if err != nil {
			// Emergency save: if we were interrupted (e.g. Ctrl+C) during inference or execution,
			// we might have partial content. Save it now using a background context
			// to ensure the write succeeds even though the main context is canceled.
			if turn.State.Response != nil && len(turn.State.Response.Parts) > 0 {
				if p, ok := e.processors[phasePersisting]; ok {
					// Use a timeout for emergency persistence to prevent hanging the shutdown sequence.
					saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					_ = p.Process(saveCtx, turn)
				}
			}
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

func (e *TurnEngine) executePhase(ctx context.Context, turn *turn) (processResult, error) {
	processor, ok := e.processors[turn.State.Phase]
	if !ok {
		return processResult{}, NewAgentError(ErrLogic, fmt.Sprintf("no processor for phase: %s", turn.State.Phase), nil)
	}

	res := processor.Process(ctx, turn)
	if res.Error != nil {
		turn.State.LastError = res.Error
	}

	next := e.determineNextPhase(turn.State.Phase, res)
	e.notifyTransition(turn.State.Phase, next, turn.State)
	turn.State.Phase = next

	if res.Error != nil && next == phaseComplete {
		return res, res.Error
	}
	return res, nil
}

func (e *TurnEngine) shouldBreak(turn *turn, res processResult) bool {
	if res.Stop {
		turn.Stop = true
	}
	return turn.Stop && turn.State.Phase != phaseComplete
}

func (e *TurnEngine) determineNextPhase(current turnPhase, res processResult) turnPhase {
	if (res.Error != nil || res.Recovery) && current != phaseRecovering {
		return phaseRecovering
	}
	if res.NextPhase != "" {
		return res.NextPhase
	}
	return phaseComplete
}

func (e *TurnEngine) notifyTransition(from, to turnPhase, state *turnState) {
	for _, h := range e.hooks {
		h.OnPhaseTransition(from, to, state)
	}
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, turn *turn) processResult {
	history, metadata, err := turn.CtxManager.Prepare(ctx, turn.Index)
	if err != nil {
		category := llm.ErrTerminal
		if IsTransient(err) {
			category = llm.ErrTransient
		}
		return processResult{Error: NewAgentError(category, "context preparation failed", err)}
	}
	turn.State.Metadata = metadata
	turn.State.Tokens = metadata.FinalTokenCount
	turn.State.PreparedHistory = history

	return processResult{NextPhase: phaseInference}
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, turn *turn) processResult {
	start := time.Now()
	respContent, metrics, err := p.invokeModel(ctx, turn)
	inferenceDuration := time.Since(start)

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
		return processResult{Error: NewAgentError(category, "inference failed", err)}
	}

	return p.routeBasedOnContent(respContent)
}

func (p *InferenceStep) invokeModel(ctx context.Context, turn *turn) (*llm.Content, *llm.Metrics, error) {
	history := turn.State.PreparedHistory
	respCh, finalize := turn.Gateway.Generate(ctx, history, turn.Registry.GetDeclarations(), turn.CtxManager.History.GetResolver())

	if turn.StreamHandler != nil {
		turn.StreamHandler(ctx, respCh)
	} else {
		for range respCh {
		}
	}

	respContent, metrics, err := finalize()
	if err != nil {
		// We return what we have (partial content) along with the error
		// so that the engine can attempt an emergency checkpoint.
		return respContent, metrics, err
	}
	if respContent == nil {
		return nil, nil, NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return respContent, metrics, nil
}

func (p *InferenceStep) updateState(turn *turn, content *llm.Content, metrics *llm.Metrics) {
	turn.State.Response = content
	turn.State.Metrics = metrics
	if metrics != nil {
		metrics.Model = turn.Model
		turn.State.Tokens = int(metrics.PromptTokens)
	}
	turn.State.HasToolCalls = p.hasToolCalls(content)
}

func (p *InferenceStep) routeBasedOnContent(content *llm.Content) processResult {
	if p.hasToolCalls(content) {
		return processResult{NextPhase: phaseExecuting}
	}
	return processResult{NextPhase: phasePersisting}
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

func (p *ExecutionStep) Process(ctx context.Context, turn *turn) processResult {
	if !turn.State.HasToolCalls {
		return processResult{NextPhase: phasePersisting}
	}

	toolStart := turn.Clock.Now()

	toolResponse, err := turn.Executor.Execute(ctx, turn.State.Response, turn.Index, turn.MaxToolTurns)
	if err != nil {
		return processResult{Error: p.handleToolExecutionError(err)}
	}

	if toolResponse != nil {
		turn.State.ToolResponse = toolResponse
		p.injectCircuitBreakerWarning(ctx, turn, toolResponse)
	}

	if turn.State.Metrics != nil {
		turn.State.Metrics.ToolDuration = turn.Clock.Now().Sub(toolStart).Seconds()
	}
	return processResult{NextPhase: phasePersisting}
}

func (p *ExecutionStep) handleToolExecutionError(err error) error {
	category := llm.ErrTerminal
	if IsTransient(err) {
		category = llm.ErrTransient
	}
	return NewAgentError(category, "tool execution failed", err)
}

func (p *ExecutionStep) injectCircuitBreakerWarning(ctx context.Context, turn *turn, toolResponse *llm.Content) {
	if toolResponse == nil {
		return
	}
	// Check if any tool triggered the circuit breaker
	for _, part := range toolResponse.Parts {
		if part.FunctionResponse != nil {
			if res, ok := part.FunctionResponse.Response["result"].(string); ok {
				if strings.Contains(res, "temporarily disabled") && strings.Contains(res, "multiple consecutive failures") {
					// Inject a safety warning into the history for the LLM
					_ = turn.CtxManager.AddContent(ctx, &llm.Content{
						Role:  "user",
						Parts: []*llm.Part{{Text: "SYSTEM WARNING: A tool has been temporarily disabled due to multiple consecutive failures. Please continue the task without attempting to use that specific tool again."}},
					})
					break
				}
			}
		}
	}
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, turn *turn) processResult {
	if turn.State.Response != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.Response); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return processResult{Error: NewAgentError(category, "history error", err)}
		}
	}

	if turn.State.ToolResponse != nil {
		if err := turn.CtxManager.AddContent(ctx, turn.State.ToolResponse); err != nil {
			category := llm.ErrTerminal
			if IsTransient(err) {
				category = llm.ErrTransient
			}
			return processResult{Error: NewAgentError(category, "failed to persist tool results", err)}
		}
	}

	return processResult{NextPhase: phaseComplete}
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct {
	Policy RetryPolicy
}

func (p *RecoveryStep) Process(ctx context.Context, turn *turn) processResult {
	err := turn.State.LastError
	if err == nil {
		return processResult{NextPhase: phaseComplete}
	}

	delay, retry := p.Policy.ShouldRetry(err, turn.State.RetryCount)
	if !retry {
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, turn, delay)
}

func (p *RecoveryStep) handleFailure(err error) processResult {
	if IsTransient(err) {
		return processResult{NextPhase: phaseComplete, Error: fmt.Errorf("max retries reached: %w", err)}
	}
	return processResult{NextPhase: phaseComplete, Error: err}
}

func (p *RecoveryStep) attemptRetry(ctx context.Context, turn *turn, delay time.Duration) processResult {
	turn.State.RetryCount++

	// Publish retry notification to the UI/EventBus
	if turn.Events != nil {
		msg := fmt.Sprintf("Transient error: %v. Retrying in %v (Attempt %d)...",
			turn.State.LastError, delay.Round(time.Millisecond), turn.State.RetryCount)
		turn.Events.Publish(events.SystemMessageEvent{
			Message: msg,
			Level:   "warn",
		})
	}

	if err := ctx.Err(); err != nil {
		return processResult{Error: err}
	}

	select {
	case <-ctx.Done():
		return processResult{Error: ctx.Err()}
	case <-turn.Clock.After(delay):
	}

	return processResult{NextPhase: phaseRefining}
}

// GetCostTracker returns the session cost tracker used by the engine.
func (e *TurnEngine) GetCostTracker() domain_pricing.ICostTracker {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.costTracker
}
