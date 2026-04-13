// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/telemetry"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"golang.org/x/sync/errgroup"

	"go.opentelemetry.io/otel"
)

// RuntimeConfig defines the runtime configuration for the Engine.
type RuntimeConfig struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
}

// engineConfig defines the lock-free runtime state for the Engine.
type engineConfig struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	CostTracker      domain_pricing.CostTracker
	SM               domain_security.Manager
	Logger           ports.Logger
}

// TurnPhase represents the current stage of a single agent Turn.
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
	Stop      bool // Explicit signal to halt the Turn
	Recovery  bool // Explicit signal that we should enter recovery
}

// RetryPolicy defines how the engine should handle errors and retries.
type RetryPolicy interface {
	ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool)
}

// DefaultRetryPolicy provides a standard retry implementation with exponential backoff and jitter.
type DefaultRetryPolicy struct {
	MaxRetries       int
	Backoff          time.Duration
	RateLimitBackoff time.Duration
}

func (p *DefaultRetryPolicy) ShouldRetry(c clock.Clock, err error, attempt int, hasSeenRateLimit bool) (time.Duration, bool) {
	if attempt >= p.MaxRetries {
		return 0, false
	}
	if isFatal(err) {
		return 0, false
	}
	if isTransient(err) {
		base := p.Backoff

		// Use the severe backoff if we have been rate-limited at any point during this Turn's
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
				if delay >= maxDelay/2 {
					delay = maxDelay
					break
				}
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

// TurnHook allows intercepting lifecycle events of a Turn.
type TurnHook interface {
	BeforeTurn(Turn *Turn)
	AfterTurn(Turn *Turn, err error)
	OnPhaseTransition(from, to TurnPhase, state *TurnState)
}

// TurnState carries data between the phases of a Turn and tracks the current phase.
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
	Execute(ctx context.Context, respContent *llm.Content, Turn int, maxToolTurns int) (*llm.Content, error)
}

// TurnProcessor defines a single stage in the TurnEngine pipeline.
type TurnProcessor interface {
	Process(ctx context.Context, Turn *Turn) (ProcessResult, error)
}

// TurnProcessorFunc is an adapter to allow the use of ordinary functions as TurnProcessors.
type TurnProcessorFunc func(context.Context, *Turn) (ProcessResult, error)

// Process calls f(ctx, Turn).
func (f TurnProcessorFunc) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	return f(ctx, Turn)
}

// TurnMiddleware wraps a TurnProcessor to inject cross-cutting concerns.
type TurnMiddleware func(TurnProcessor) TurnProcessor

// Turn carries state and configuration for a single agent Turn.
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
	Logger       ports.Logger

	// Results/Outputs
	Stop bool
}

// Engine manages the "Think -> Act -> Observe" cycle using a state machine.
type Engine struct {
	mu           sync.RWMutex
	config       atomic.Pointer[engineConfig]
	ctxManager   *session.ContextManager
	gateway      llm.LLMGateway
	executor     ToolExecutor
	registry     tools.Registry
	tokenCounter llm.TokenCounter
	events       events.EventBus
	turnsLogger  ports.TurnsLogger // Optional turns logger for coordinated telemetry
	processors   map[TurnPhase]TurnProcessor
	middleware   []TurnMiddleware
	hooks        []TurnHook
	RetryPolicy  RetryPolicy
	clock        clock.Clock
}

// EngineOption allows configuring the Engine.
type EngineOption func(*Engine, *engineConfig)

// WithEngineClock sets a custom clock implementation.
func WithEngineClock(c clock.Clock) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.clock = c
	}
}

// WithEngineCostTracker sets the cost tracker for the engine.
func WithEngineCostTracker(tracker domain_pricing.CostTracker) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.CostTracker = tracker
	}
}

// WithEngineConfig sets the security and usage configuration for the engine.
func WithEngineConfig(sm domain_security.Manager, providerName, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.SM = sm
		cfg.ProviderName = providerName
		cfg.Model = model
		cfg.Mode = mode
		cfg.PricingOverrides = pricingOverrides
	}
}

// WithEngineLogger sets the logger for the engine.
func WithEngineLogger(l ports.Logger) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.Logger = l
	}
}

// ApplyOptions applies new options to the engine.
func (e *Engine) ApplyOptions(opts ...EngineOption) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for {
		oldCfg := e.config.Load()
		newCfg := *oldCfg // shallow copy
		for _, opt := range opts {
			opt(e, &newCfg)
		}
		if e.config.CompareAndSwap(oldCfg, &newCfg) {
			break
		}
	}
}

// Reconfigure propagates configuration changes to the engine.
func (e *Engine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.CostTracker) {
	for {
		oldCfg := e.config.Load()
		newCfg := *oldCfg
		newCfg.ProviderName = cfg.ProviderName
		newCfg.Model = cfg.Model
		newCfg.Mode = cfg.Mode
		newCfg.PricingOverrides = cfg.PricingOverrides
		newCfg.CostTracker = tracker
		if e.config.CompareAndSwap(oldCfg, &newCfg) {
			break
		}
	}
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
		RetryPolicy:  &DefaultRetryPolicy{MaxRetries: 6, Backoff: 2 * time.Second, RateLimitBackoff: 5 * time.Second},
		clock:        clock.RealClock{},
	}

	cfg := &engineConfig{}

	// Register default processors
	e.processors[PhaseGuard] = &GuardStep{}
	e.processors[PhaseRefining] = &ContextRefiner{}
	e.processors[PhaseInference] = &InferenceStep{}
	e.processors[PhaseExecuting] = &ExecutionStep{}
	e.processors[PhasePersisting] = &PersistenceStep{}
	e.processors[PhaseRecovering] = &RecoveryStep{Policy: e.RetryPolicy}

	for _, opt := range opts {
		opt(e, cfg)
	}

	e.config.Store(cfg)

	// Ensure RecoveryStep uses the (potentially overridden) policy
	if rs, ok := e.processors[PhaseRecovering].(*RecoveryStep); ok {
		rs.Policy = e.RetryPolicy
	}

	// Default middleware for eventing if bus is provided
	if e.events != nil {
		e.middleware = append(e.middleware,
			e.withMetrics(),
			e.withStatusReporter(),
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

// Run executes the multi-Turn orchestration loop.
func (e *Engine) Run(ctx context.Context, startTime time.Time) error {
	sessionToolCallCount := make(map[string]int)
	Turn := e.CreateTurn(0, startTime)
	Turn.State.ToolCallCount = sessionToolCallCount

	for Turn.State.Phase != PhaseComplete {
		err := e.ExecuteTurn(ctx, Turn)
		if err != nil {
			return err
		}

		if e.shouldStopRunning(Turn) {
			break
		}

		// Prepare for next Turn
		e.PrepareNextTurn(Turn)
	}
	return nil
}

func (e *Engine) PrepareNextTurn(Turn *Turn) {
	Turn.Index++
	Turn.State.CurrentTurns = Turn.Index
	Turn.State.Phase = PhaseGuard
	Turn.State.RetryCount = 0
	Turn.State.Response = nil
	Turn.State.ToolResponse = nil
	Turn.State.HasToolCalls = false
	Turn.State.ToolReasons = nil
}

func (e *Engine) CreateTurn(index int, startTime time.Time) *Turn {
	cfg := e.config.Load()
	counter := e.tokenCounter

	Turn := &Turn{
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
		CostTracker:  cfg.CostTracker,
		ProviderName: cfg.ProviderName,
		Model:        cfg.Model,
		Mode:         cfg.Mode,
		Logger:       e.getLogger(),
	}
	Turn.MaxToolTurns = e.ctxManager.GetLimits().MaxToolTurns
	return Turn
}

func (e *Engine) notifyBeforeTurn(Turn *Turn) {
	e.mu.RLock()
	hooks := make([]TurnHook, len(e.hooks))
	copy(hooks, e.hooks)
	e.mu.RUnlock()
	for _, h := range hooks {
		h.BeforeTurn(Turn)
	}
}

func (e *Engine) notifyAfterTurn(Turn *Turn, err error) {
	e.mu.RLock()
	hooks := make([]TurnHook, len(e.hooks))
	copy(hooks, e.hooks)
	e.mu.RUnlock()
	for _, h := range hooks {
		h.AfterTurn(Turn, err)
	}
}

func (e *Engine) shouldStopRunning(Turn *Turn) bool {
	return !Turn.State.HasToolCalls || Turn.Stop
}

func (e *Engine) ExecuteTurn(parentCtx context.Context, Turn *Turn) error {
	ctx, span := otel.Tracer("agent").Start(parentCtx, "agent.Turn")
	defer span.End()

	trace := telemetry.NewTurnTrace()
	ctxWithTrace := telemetry.ContextWithTrace(ctx, trace)

	e.notifyBeforeTurn(Turn)

	err := e.runPhaseLoop(ctxWithTrace, Turn)

	e.finalizeTurnTrace(trace, err)
	if err := events.SafePublish(ctx, e.events, events.TraceEvent{Trace: trace}); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			e.getLogger().Error("event_publish_failed",
				"event_type", "TraceEvent",
				"error", err)
		}
	}

	e.notifyAfterTurn(Turn, err)
	return err
}

func (e *Engine) runPhaseLoop(ctx context.Context, Turn *Turn) error {
	for Turn.State.Phase != PhaseComplete {
		res, err := e.ExecutePhase(ctx, Turn)
		if err != nil && Turn.State.Phase == PhaseComplete {
			e.EmergencySave(Turn)
			return err
		}
		if res.Stop {
			Turn.Stop = true
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

func (e *Engine) EmergencySave(Turn *Turn) {
	if Turn.State.Response != nil && len(Turn.State.Response.Parts) > 0 {
		e.mu.RLock()
		p, ok := e.processors[PhasePersisting]
		e.mu.RUnlock()
		if ok {
			saveCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = p.Process(saveCtx, Turn)
		}
	}
}

func (e *Engine) ExecutePhase(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	e.mu.RLock()
	processor, ok := e.processors[Turn.State.Phase]
	e.mu.RUnlock()

	if !ok {
		Turn.State.Phase = PhaseComplete // Force exit to prevent infinite loop in runPhaseLoop
		return ProcessResult{}, NewAgentError(ErrLogic, fmt.Sprintf("no processor for phase: %s", Turn.State.Phase), nil)
	}

	res, err := processor.Process(ctx, Turn)
	if err != nil {
		Turn.State.LastError = err
	}

	next := e.determineNextPhase(Turn.State.Phase, res, err)
	e.notifyTransition(Turn.State.Phase, next, Turn.State)
	Turn.State.Phase = next

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
	e.mu.RLock()
	hooks := make([]TurnHook, len(e.hooks))
	copy(hooks, e.hooks)
	e.mu.RUnlock()
	for _, h := range hooks {
		h.OnPhaseTransition(from, to, state)
	}
}

// GuardStep validates the Turn against limits before proceeding.
type GuardStep struct{}

func (p *GuardStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	maxTurns := Turn.CtxManager.GetLimits().MaxToolTurns
	if Turn.Index > maxTurns {
		return ProcessResult{}, NewAgentError(llm.ErrTerminal, fmt.Sprintf("turn %d exceeds limit %d", Turn.Index, maxTurns), llm.ErrMaxTurnsReached)
	}

	evt := events.TurnStarted{Turn: Turn.Index, MaxTurns: maxTurns}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if errors.Is(err, events.ErrBusNotInitialized) {
			return ProcessResult{NextPhase: PhaseRefining}, nil
		}
		Turn.getLogger().Error("event_publish_failed",
			"event_type", string(evt.Type()),
			"error", err)
		return ProcessResult{}, err
	}
	return ProcessResult{NextPhase: PhaseRefining}, nil
}

// ContextRefiner prepares the context for the LLM call.
type ContextRefiner struct{}

func (p *ContextRefiner) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	history, metadata, err := Turn.CtxManager.Prepare(ctx, Turn.Index)
	if err != nil {
		category := llm.ErrTerminal
		if isTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "context preparation failed", err)
	}
	Turn.State.Metadata = metadata
	Turn.State.Tokens = metadata.FinalTokenCount
	Turn.State.PreparedHistory = history

	return ProcessResult{NextPhase: PhaseInference}, nil
}

// InferenceStep calls the LLM.
type InferenceStep struct{}

func (p *InferenceStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	start := Turn.Clock.Now()
	respContent, metrics, err := p.invokeModel(ctx, Turn)
	inferenceDuration := Turn.Clock.Now().Sub(start)

	if trace := telemetry.TraceFromContext(ctx); trace != nil {
		trace.InferenceDuration = inferenceDuration
	}

	if respContent != nil {
		p.updateState(Turn, respContent, metrics)
	}

	if err != nil {
		category := llm.ErrTerminal
		if isTransient(err) {
			category = llm.ErrTransient
		}
		return ProcessResult{}, NewAgentError(category, "inference failed", err)
	}

	return p.routeBasedOnContent(respContent), nil
}

func (p *InferenceStep) invokeModel(ctx context.Context, Turn *Turn) (respContent *llm.Content, metrics *llm.Metrics, err error) {
	_ = events.SafePublish(ctx, Turn.Events, events.InferenceStartedEvent{Model: Turn.Model})

	defer func() {
		safeContent := respContent
		if safeContent == nil {
			safeContent = &llm.Content{Role: "model"}
		}
		// Detach context to ensure the UI ALWAYS receives the stop signal even on timeout
		stopCtx := context.WithoutCancel(ctx)
		if err := events.SafePublish(stopCtx, Turn.Events, events.ResponseEvent{Content: safeContent}); err != nil {
			Turn.getLogger().Error("Failed to publish ResponseEvent; UI spinner may hang", "error", err)
		}
	}()

	var activeToolkits []string
	if Turn.CtxManager != nil && Turn.CtxManager.SessionProvider != nil {
		activeToolkits = Turn.CtxManager.SessionProvider.GetInfo().ActiveToolkits
	}

	var activeTools []*tools.ToolDeclaration
	if len(activeToolkits) > 0 {
		activeTools = Turn.Registry.GetDeclarationsByToolkits(activeToolkits)
	} else {
		activeTools = Turn.Registry.GetCoreDeclarations()
	}

	respContent, metrics, err = Turn.Gateway.Generate(ctx, Turn.State.PreparedHistory, activeTools, Turn.CtxManager.History.GetResolver())
	if err == nil && respContent == nil {
		return nil, nil, NewAgentError(ErrLogic, "api returned nil content", nil)
	}
	return respContent, metrics, err
}

func (p *InferenceStep) updateState(Turn *Turn, content *llm.Content, metrics *llm.Metrics) {
	Turn.State.Response = content
	Turn.State.Metrics = metrics
	if metrics != nil {
		metrics.Model = Turn.Model
		metrics.Provider = Turn.ProviderName
		Turn.State.Tokens = int(metrics.PromptTokens)
	}
	Turn.State.HasToolCalls = p.hasToolCalls(content)
	if Turn.State.HasToolCalls {
		// Preallocate capacity based on the number of parts in the response
		Turn.State.ToolReasons = make([]string, 0, len(content.Parts))
		for _, part := range content.Parts {
			if part.FunctionCall != nil {
				if reason, ok := part.FunctionCall.Args["reason"].(string); ok && reason != "" {
					Turn.State.ToolReasons = append(Turn.State.ToolReasons, reason)
				}
			}
		}
	}
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

func (p *ExecutionStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if !Turn.State.HasToolCalls {
		return ProcessResult{NextPhase: PhasePersisting}, nil
	}

	var names []string
	if Turn.State.Response != nil {
		names = make([]string, 0, len(Turn.State.Response.Parts))
		for _, part := range Turn.State.Response.Parts {
			if part.FunctionCall != nil {
				names = append(names, part.FunctionCall.Name)
			}
		}
	}
	_ = events.SafePublish(ctx, Turn.Events, events.ToolExecutionStartedEvent{ToolNames: names})

	toolStart := Turn.Clock.Now()

	toolResponse, err := Turn.Executor.Execute(ctx, Turn.State.Response, Turn.Index, Turn.MaxToolTurns)

	if toolResponse != nil {
		Turn.State.ToolResponse = toolResponse
		p.validatePayloadLimits(ctx, Turn)
	}

	if err != nil {
		return ProcessResult{}, p.handleToolExecutionError(err)
	}

	if Turn.State.Metrics != nil {
		Turn.State.Metrics.ToolDuration = Turn.Clock.Now().Sub(toolStart).Seconds()
		if trace := telemetry.TraceFromContext(ctx); trace != nil {
			Turn.State.Metrics.CumulativeToolDuration = trace.CumulativeToolDuration().Seconds()
		}
	}
	return ProcessResult{NextPhase: PhasePersisting}, nil
}

func (p *ExecutionStep) handleToolExecutionError(err error) error {
	category := llm.ErrTerminal
	if isTransient(err) {
		category = llm.ErrTransient
	}
	return NewAgentError(category, "tool execution failed", err)
}

// PersistenceStep saves the response and tool results to history.
type PersistenceStep struct{}

func (p *PersistenceStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	if Turn.State.Response != nil {
		if err := Turn.CtxManager.AddContent(ctx, Turn.State.Response); err != nil {
			category := llm.ErrTerminal
			if isTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "history error", err)
		}
	}

	if Turn.State.ToolResponse != nil {
		if err := Turn.CtxManager.AddContent(ctx, Turn.State.ToolResponse); err != nil {
			category := llm.ErrTerminal
			if isTransient(err) {
				category = llm.ErrTransient
			}
			return ProcessResult{}, NewAgentError(category, "failed to persist tool results", err)
		}
	}

	return ProcessResult{NextPhase: PhaseComplete}, nil
}

// RecoveryStep handles errors by deciding whether to retry or fail.
type RecoveryStep struct {
	Policy RetryPolicy
}

func (p *RecoveryStep) Process(ctx context.Context, Turn *Turn) (ProcessResult, error) {
	err := Turn.State.LastError
	if err == nil {
		return ProcessResult{NextPhase: PhaseComplete}, nil
	}

	// State mutation is handled by the workflow engine (caller)
	isRateLimit := errors.Is(err, llm.ErrRateLimit)
	if isRateLimit {
		Turn.State.HasSeenRateLimit = true
	}

	delay, retry := p.Policy.ShouldRetry(Turn.Clock, err, Turn.State.RetryCount, Turn.State.HasSeenRateLimit)
	if !retry {
		return p.handleFailure(err)
	}

	return p.attemptRetry(ctx, Turn, delay)
}

func (p *RecoveryStep) handleFailure(err error) (ProcessResult, error) {
	if isTransient(err) {
		return ProcessResult{NextPhase: PhaseComplete}, fmt.Errorf("max retries reached: %w", err)
	}
	return ProcessResult{NextPhase: PhaseComplete}, err
}

func (p *RecoveryStep) attemptRetry(ctx context.Context, Turn *Turn, delay time.Duration) (ProcessResult, error) {
	Turn.State.RetryCount++

	// Log retry to application logs (Technical debugging only)
	Turn.getLogger().Debug("retrying_after_transient_error",
		"error", Turn.State.LastError,
		"delay", delay,
		"attempt", Turn.State.RetryCount)

	// Publish retry notification to the UI/EventBus
	msg := fmt.Sprintf("Transient error: %v. Retrying in %v (Attempt %d)...",
		Turn.State.LastError, delay.Round(time.Millisecond), Turn.State.RetryCount)
	evt := events.SystemMessageEvent{
		Message: msg,
		Level:   "warn",
	}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			Turn.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
			return ProcessResult{}, err
		}
	}

	// Publish RetryWaitingEvent to show the spinner during the backoff delay
	_ = events.SafePublish(ctx, Turn.Events, events.RetryWaitingEvent{Duration: delay})

	if err := ctx.Err(); err != nil {
		return ProcessResult{}, err
	}

	select {
	case <-ctx.Done():
		return ProcessResult{}, ctx.Err()
	case <-Turn.Clock.After(delay):
	}

	return ProcessResult{NextPhase: PhaseRefining}, nil
}

func (p *ExecutionStep) validatePayloadLimits(ctx context.Context, Turn *Turn) {
	if Turn.State.ToolResponse == nil || Turn.CtxManager == nil || Turn.CtxManager.Strategy == nil {
		return
	}

	limits := Turn.CtxManager.GetLimits()
	if limits.MaxHistoryTokens <= 0 {
		return
	}

	toolTokens := Turn.TokenCounter.Count([]*llm.Content{Turn.State.ToolResponse})
	isTooLarge, instruction := p.checkTokenBudget(Turn, toolTokens, limits)

	if isTooLarge {
		p.handleOversizedPayload(ctx, Turn, toolTokens, instruction)
	}
}

func (p *ExecutionStep) checkTokenBudget(Turn *Turn, toolTokens int, limits events.Limits) (bool, string) {
	// We use the remaining buffer, accounting for the 10% system reservation
	maxAllowed := int(float64(limits.MaxHistoryTokens) * 0.90)

	// Cap individual tool response size to 50% of total limit just in case,
	// AND ensure it doesn't push the total over the cliff.
	if toolTokens > int(float64(limits.MaxHistoryTokens)*0.50) {
		return true, "The individual tool output is too massive. You MUST use precise boundaries (e.g., 'tail_lines', 'max_lines', 'limit', or 'grep'). Summarizing history will not fix this."
	} else if Turn.State.Tokens+toolTokens > maxAllowed {
		return true, "The total conversation context is nearly exhausted. Please call 'summarize_history' first to free up space, then run the tool again."
	}

	return false, ""
}

func (p *ExecutionStep) handleOversizedPayload(ctx context.Context, Turn *Turn, toolTokens int, instruction string) {
	// Delegate mutation to the utility with context-aware instruction
	truncateOversizedResponse(Turn.State.ToolResponse, toolTokens, instruction)

	evt := events.SystemMessageEvent{
		Message: fmt.Sprintf("Tool output truncated (~%d tokens) to prevent exceeding safety limit.", toolTokens),
		Level:   "error",
	}
	if err := events.SafePublish(ctx, Turn.Events, evt); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			Turn.getLogger().Error("event_publish_failed",
				"event_type", string(evt.Type()),
				"error", err)
		}
	}
}

func (e *Engine) getLogger() ports.Logger {
	cfg := e.config.Load()
	if cfg != nil && cfg.Logger != nil {
		return cfg.Logger
	}
	return slog.Default()
}

func (t *Turn) getLogger() ports.Logger {
	if t.Logger != nil {
		return t.Logger
	}
	return slog.Default()
}

// StartTelemetry coordinates the lifecycle of background listeners and telemetry workers.
// Implementation follows the coordinated concurrency pattern using errgroup.
func (e *Engine) StartTelemetry(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	if e.events != nil {
		g.Go(func() error {
			// Listen blocks until ctx.Done(), then returns
			return e.events.Listen(ctx)
		})
	}

	if e.turnsLogger != nil {
		g.Go(func() error {
			// Listen blocks until ctx.Done(), then returns
			return e.turnsLogger.Listen(ctx)
		})
	}

	// Wait for the background worker to shut down cleanly when ctx is canceled
	return g.Wait()
}

// WithEngineTurnsLogger sets the turns logger for the engine.
func WithEngineTurnsLogger(tl ports.TurnsLogger) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.turnsLogger = tl
	}
}

// WithEngineMiddleware adds middleware to the engine.
func WithEngineMiddleware(m ...TurnMiddleware) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithEngineProcessor overrides a phase processor.
func WithEngineProcessor(phase TurnPhase, p TurnProcessor) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.processors[phase] = p
	}
}

// WithEngineHook adds a Turn hook to the engine.
func WithEngineHook(h TurnHook) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.hooks = append(e.hooks, h)
	}
}

// WithEngineRetryPolicy sets the retry policy for the engine.
func WithEngineRetryPolicy(p RetryPolicy) EngineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.RetryPolicy = p
	}
}
