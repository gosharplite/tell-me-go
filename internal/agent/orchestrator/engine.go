// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
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
	middleware   []turnMiddleware
	hooks        []TurnHook
	RetryPolicy  RetryPolicy
	clock        clock.Clock
}

// engineOption allows configuring the Engine.
type engineOption func(*Engine, *engineConfig)

// WithEngineClock sets a custom clock implementation.
func WithEngineClock(c clock.Clock) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.clock = c
	}
}

// WithEngineCostTracker sets the cost tracker for the engine.
func WithEngineCostTracker(tracker domain_pricing.CostTracker) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.CostTracker = tracker
	}
}

// WithEngineConfig sets the security and usage configuration for the engine.
func WithEngineConfig(sm domain_security.Manager, providerName, model, mode string, pricingOverrides map[string]domain_pricing.ModelPricing) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.SM = sm
		cfg.ProviderName = providerName
		cfg.Model = model
		cfg.Mode = mode
		cfg.PricingOverrides = pricingOverrides
	}
}

// WithEngineLogger sets the logger for the engine.
func WithEngineLogger(l ports.Logger) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		cfg.Logger = l
	}
}

// ApplyOptions applies new options to the engine.
func (e *Engine) ApplyOptions(opts ...engineOption) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldCfg := e.config.Load()
	newCfg := *oldCfg
	for _, opt := range opts {
		opt(e, &newCfg)
	}
	e.config.Store(&newCfg)
}

// Reconfigure propagates configuration changes to the engine.
func (e *Engine) Reconfigure(cfg RuntimeConfig, tracker domain_pricing.CostTracker) {
	e.mu.Lock()
	defer e.mu.Unlock()

	oldCfg := e.config.Load()
	newCfg := *oldCfg
	newCfg.ProviderName = cfg.ProviderName
	newCfg.Model = cfg.Model
	newCfg.Mode = cfg.Mode
	newCfg.PricingOverrides = cfg.PricingOverrides
	newCfg.CostTracker = tracker
	e.config.Store(&newCfg)
}

// NewEngine creates a new Engine with a default pipeline.
func NewEngine(gw llm.LLMGateway, ex ToolExecutor, cm *session.ContextManager, reg tools.Registry, bus events.EventBus, counter llm.TokenCounter, opts ...engineOption) *Engine {
	backoff := 2 * time.Second
	rateLimitBackoff := 5 * time.Second

	// Architectural Speed optimization for E2E tests:
	// Use near-zero backoff when TELL_ME_FAST_RETRY=1 to prevent artificial delays.
	if os.Getenv("TELL_ME_FAST_RETRY") == "1" {
		backoff = 1 * time.Millisecond
		rateLimitBackoff = 1 * time.Millisecond
	}

	e := &Engine{
		gateway:      gw,
		executor:     ex,
		ctxManager:   cm,
		registry:     reg,
		tokenCounter: counter,
		events:       bus,
		processors:   make(map[TurnPhase]TurnProcessor),
		RetryPolicy:  &DefaultRetryPolicy{MaxRetries: 6, Backoff: backoff, RateLimitBackoff: rateLimitBackoff},
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
		e.prepareNextTurn(Turn)
	}
	return nil
}

func (e *Engine) prepareNextTurn(Turn *Turn) {
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
		res, err := e.executePhase(ctx, Turn)
		if err != nil && Turn.State.Phase == PhaseComplete {
			e.emergencySave(Turn)
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

func (e *Engine) emergencySave(Turn *Turn) {
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

func (e *Engine) executePhase(ctx context.Context, Turn *Turn) (ProcessResult, error) {
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

func (e *Engine) Processors() map[TurnPhase]TurnProcessor {
	return e.processors
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
func WithEngineTurnsLogger(tl ports.TurnsLogger) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.turnsLogger = tl
	}
}

// WithEngineMiddleware adds middleware to the engine.
func WithEngineMiddleware(m ...turnMiddleware) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.middleware = append(e.middleware, m...)
	}
}

// WithEngineProcessor overrides a phase processor.
func WithEngineProcessor(phase TurnPhase, p TurnProcessor) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.processors[phase] = p
	}
}

// WithEngineHook adds a Turn hook to the engine.
func WithEngineHook(h TurnHook) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.hooks = append(e.hooks, h)
	}
}

// WithEngineRetryPolicy sets the retry policy for the engine.
func WithEngineRetryPolicy(p RetryPolicy) engineOption {
	return func(e *Engine, cfg *engineConfig) {
		e.RetryPolicy = p
	}
}
