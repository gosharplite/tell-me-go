// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"

	"github.com/gosharplite/tell-me-go/internal/agent/executor"
	agentsmemory "github.com/gosharplite/tell-me-go/internal/agent/memory"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	agentskills "github.com/gosharplite/tell-me-go/internal/agent/skills"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
	"sync"
)

// runtimeConfig consolidates all agent configuration parameters.
type runtimeConfig struct {
	Limits           events.Limits
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	Memory           domain_config.MemoryConfig
}

// agent represents the chat orchestration logic (Stateless Service).
type agent struct {
	gateway       domain_llm.LLMGateway
	engine        *orchestrator.Engine
	ctxManager    *sessctx.Manager
	configWatcher domain_config.ConfigWatcher
	strategy      *sessctx.Strategy
	executor      *executor.Dispatcher // write-only: assigned in initComponents; the dispatcher value is consumed by NewEngine via the local
	events        events.EventBus
	tracker       domain_pricing.CostTracker
	turnsLogger   ports.TurnsLogger
	logger        ports.Logger

	// Dependencies held for initialization
	hManager         ports.HistoryManager
	registry         tools.Registry
	sm               domain_security.Manager
	providerName     string
	clock            clock.Clock
	summarizer       ports.Summarizer
	skillSelector    skills.SkillSelector
	sessionProvider  ports.SessionProvider
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	registerInternal bool
	initCtx          context.Context
	ecosystemIntro   string

	// memoryClient and memorySeed are the memory integration seam (ADR-068):
	// the MCP client for MEMORY.SERVER plus the seed MemoryConfig. They are
	// stored here by WithMemoryClient for the later injector/hook wiring
	// task; no behavior consumes them yet. A nil client is legal and yields
	// an inert memory integration.
	memoryClient tools.MCPClient
	memorySeed   domain_config.MemoryConfig

	// memoryCfg is the shared hot-reload snapshot for the memory components
	// (plurInjector/plurHook). prepareRuntimeConfig refreshes it lock-free per
	// turn from the ConfigWatcher (ADR-068 §6) — the pointer itself is stored
	// at initComponents and read by the components on every Transform/AfterTurn.
	memoryCfg atomic.Pointer[domain_config.MemoryConfig]
	// memoryHook is the plurHook TurnHook, registered once at initComponents
	// via NewEngine opts — never per-Chat (WithEngineHook appends → double-fire).
	memoryHook orchestrator.TurnHook

	config atomic.Pointer[runtimeConfig]
}

// NewAgent creates a new agent with required dependencies.
func NewAgent(client domain_llm.LLMGateway, bus events.EventBus, registry tools.Registry, opts ...AgentOption) (ports.Chatter, error) {
	a := &agent{
		gateway:  client,
		events:   bus,
		registry: registry,
		// Defaults
		logger:  slog.Default(),
		clock:   clock.RealClock{},
		initCtx: context.Background(),
	}

	for _, opt := range opts {
		opt(a)
	}

	if err := a.initComponents(); err != nil {
		return nil, err
	}

	if a.registerInternal {
		if err := session.RegisterInternal(a.registry, a.ctxManager, a.getLogger()); err != nil {
			return nil, fmt.Errorf("failed to register internal tools: %w", err)
		}
	}

	if err := a.applyConfig(a.initCtx); err != nil {
		return nil, fmt.Errorf("failed to apply initial configuration: %w", err)
	}
	return a, nil
}

func (a *agent) initComponents() error {
	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(a.registry))
	a.strategy = strategy

	exec, err := executor.NewPipelineDispatcher(a.registry, a.sm, a.events, a.logger, &executor.TelemetryLogger{})
	if err != nil {
		return fmt.Errorf("failed to create tool executor: %w", err)
	}
	a.executor = exec

	if a.configWatcher == nil {
		a.configWatcher = domain_config.NewNoOpConfigWatcher(
			domain_config.DefaultMaxHistoryTokens,
			domain_config.DefaultMaxToolTurns,
			domain_config.DefaultMaxHistoryTurns,
		)
	}

	a.config.Store(&runtimeConfig{
		ProviderName:     a.providerName,
		Model:            a.model,
		Mode:             a.mode,
		PricingOverrides: a.pricingOverrides,
		Limits: events.Limits{
			MaxHistoryTokens: domain_config.DefaultMaxHistoryTokens,
			MaxToolTurns:     domain_config.DefaultMaxToolTurns,
			MaxHistoryTurns:  domain_config.DefaultMaxHistoryTurns,
		},
	})

	var injectorOpts []agentskills.SkillInjectorOption
	if a.ecosystemIntro != "" {
		injectorOpts = append(injectorOpts, agentskills.WithSkillEcosystemIntro(a.ecosystemIntro))
	}

	factory := &sessctx.Factory{
		Registry:   a.registry,
		History:    a.hManager,
		Summarizer: a.summarizer,
		Estimator:  strategy,
		Events:     a.events,
		Extras:     []sessctx.ContextTransformer{agentskills.NewSkillInjector(a.skillSelector, a.logger, injectorOpts...)},
	}

	// Memory integration (ADR-068 §6): construct once, at initComponents —
	// never per-Chat (WithEngineHook appends → double-fire). Enabled-but-
	// absent-server → warn and disable (two-stage fallback): a nil client
	// yields an inert, stable DI shape; the runtime nil-client guards fail
	// open.
	if a.memoryClient != nil || (a.memorySeed.Enabled && a.memorySeed.Server != "") {
		if a.memoryClient == nil {
			a.getLogger().Warn("memory_server_unavailable", "server", a.memorySeed.Server)
			a.memorySeed.Enabled = false // effective disable — inert DI shape
		}
		a.memoryCfg.Store(&a.memorySeed)
		factory.Extras = append(factory.Extras, agentsmemory.NewPlurInjector(a.memoryClient, &a.memoryCfg, a.logger))
		a.memoryHook = agentsmemory.NewPlurHook(a.memoryClient, &a.memoryCfg, a.logger, a.clock, a.hManager)
	}

	a.ctxManager = sessctx.NewManager(strategy, a.hManager, a.events, factory,
		sessctx.WithLogger(a.logger),
		sessctx.WithSessionProvider(a.sessionProvider),
	)

	// Initialize engine
	initialCfg := a.config.Load()
	engineOpts := []orchestrator.EngineOption{
		orchestrator.WithEngineConfig(a.sm, initialCfg.ProviderName, initialCfg.Model, initialCfg.Mode, initialCfg.PricingOverrides),
		orchestrator.WithEngineCostTracker(a.tracker),
		orchestrator.WithEngineLogger(a.logger),
		orchestrator.WithEngineTurnsLogger(a.turnsLogger),
		orchestrator.WithEngineClock(a.clock),
	}
	if a.memoryHook != nil {
		engineOpts = append(engineOpts, orchestrator.WithEngineHook(a.memoryHook))
	}
	a.engine = orchestrator.NewEngine(a.gateway, exec, a.ctxManager, a.registry, a.events, strategy, engineOpts...)

	return nil
}

// applyConfig is the hot-reload entrypoint for runtime configuration changes
// (provider switch, context-window resize, tool toggle, history-limit changes).
// It is invoked from three call sites: NewAgent (initial application),
// SetLimits (runtime updates), and Chat (pre-flight refresh per turn).
//
// Per ADR-029, applyConfig implements a fail-fast delegate chain in fixed
// order: SafePublish(ConfigUpdated) → Engine.Reconfigure → Manager.Reconfigure.
// Any delegate returning an error short-circuits the chain via `return err`,
// leaving downstream delegates uncalled. There is no rollback: each delegate
// validates its input before mutating internal state, so a failure leaves
// the corresponding component on its previous configuration (ADR-029 §4).
//
// The order is contractual, not stylistic. See the inline comment above the
// delegate sequence below for the rationale. Do not reorder these calls.
func (a *agent) applyConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	cfg := a.prepareRuntimeConfig()
	newCfg := &cfg
	tracker := a.tracker

	// ADR-029 fail-fast delegate chain. The three calls below MUST execute
	// in this exact order:
	//
	//   1. events.SafePublish(ConfigUpdated)  — broadcast new limits to subscribers
	//   2. Engine.Reconfigure                 — apply runtime config to orchestrator
	//   3. Manager.Reconfigure                — apply limits to context manager
	//
	// Each call returns error and short-circuits the chain on failure. The
	// ordering reflects dependency direction: subscribers receive the new
	// configuration before the engine acts on it; the engine's runtime state
	// is consistent before the context manager rebuilds its pipeline. Reordering
	// would cause subscribers to react to engine state that does not yet exist,
	// or cause the context manager to rebuild against stale engine config.
	//
	// Do not reorder, parallelize, or wrap in errgroup. Do not "tidy" by
	// collapsing the if-blocks. The structural sequence IS the contract.
	if err := a.publishConfigUpdate(ctx, newCfg); err != nil {
		return err
	}

	if err := a.reconfigureEngine(newCfg, tracker); err != nil {
		return err
	}
	if err := a.reconfigureContextManager(newCfg); err != nil {
		return err
	}
	return nil
}

// prepareRuntimeConfig assembles the next runtime configuration by
// refreshing the config watcher, reading new limits, syncing to the
// strategy, and atomically storing the result. It returns a value
// copy of the stored config so that delegates receive a stack-local
// pointer — never the atomic store's address.
func (a *agent) prepareRuntimeConfig() runtimeConfig {
	oldCfg := a.config.Load()
	a.configWatcher.Refresh(oldCfg.Model)

	tokens, toolTurns, histTurns := a.configWatcher.GetLimits()

	newCfg := *oldCfg // shallow copy
	newCfg.Limits.MaxHistoryTokens = tokens
	newCfg.Limits.MaxToolTurns = toolTurns
	newCfg.Limits.MaxHistoryTurns = histTurns
	newCfg.Memory = a.configWatcher.GetMemoryConfig()

	if a.strategy != nil {
		a.strategy.SetLimits(tokens, toolTurns, histTurns)
		a.strategy.SetContextWindow(a.configWatcher.GetContextWindow())
	}

	a.config.Store(&newCfg)
	// Memory propagation is pre-chain (before the ADR-029 delegates), like
	// Limits: the shared atomic pointer gives plurInjector/plurHook a lock-free
	// per-turn read of the hot-reloaded MEMORY config. Zero ADR-029 changes.
	a.memoryCfg.Store(&newCfg.Memory)
	return newCfg
}

// publishConfigUpdate broadcasts the new runtime configuration to
// event subscribers via SafePublish. Per ADR-029 §3, this is the
// first step of the fail-fast delegate chain.
//
// ErrBusNotInitialized is tolerated: if the event bus was never
// initialized (e.g. bare agent construction), the publish is
// silently skipped. All other errors are logged and returned.
func (a *agent) publishConfigUpdate(ctx context.Context, cfg *runtimeConfig) error {
	if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: cfg.Limits}); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			a.getLogger().Error("event_publish_failed",
				"event_type", "ConfigUpdated",
				"error", err)
			return err
		}
	}
	return nil
}

// reconfigureEngine applies the runtime configuration to the
// orchestrator engine. Per ADR-029 §3, this is the second step of
// the fail-fast delegate chain.
//
// A nil engine (e.g. bare agent before full initialization) is not
// an error — the call is silently skipped.
func (a *agent) reconfigureEngine(cfg *runtimeConfig, tracker domain_pricing.CostTracker) error {
	if a.engine == nil {
		return nil
	}
	return a.engine.Reconfigure(orchestrator.RuntimeConfig{
		ProviderName:     cfg.ProviderName,
		Model:            cfg.Model,
		Mode:             cfg.Mode,
		PricingOverrides: cfg.PricingOverrides,
	}, tracker)
}

// reconfigureContextManager applies the new limits to the session
// context manager. Per ADR-029 §3, this is the third and final
// step of the fail-fast delegate chain.
//
// A nil context manager (e.g. bare agent before full initialization)
// is not an error — the call is silently skipped.
func (a *agent) reconfigureContextManager(cfg *runtimeConfig) error {
	if a.ctxManager == nil {
		return nil
	}
	return a.ctxManager.Reconfigure(cfg.Limits)
}

func (a *agent) Subscribe(sub func(context.Context, events.Event)) {
	a.events.Subscribe(sub)
}

func (a *agent) emit(ctx context.Context, e events.Event) {
	// [SCALABILITY FIX] Always use a bounded context for publishing events
	// to prevent cascading system deadlocks if a subscriber stalls.
	if err := events.SafePublish(ctx, a.events, e); err != nil {
		if !errors.Is(err, events.ErrBusNotInitialized) {
			a.getLogger().Error("event_publish_failed",
				"event_type", string(e.Type()),
				"error", err)
		}
	}
}

// SetLimits sets the operational limits for the agent.
// It returns an error if the configuration cannot be applied (e.g., context cancellation).
func (a *agent) SetLimits(ctx context.Context, toolTurns, historyTokens, historyTurns int) error {
	a.configWatcher.SetLimits(historyTokens, toolTurns, historyTurns)
	return a.applyConfig(ctx)
}

// memoryFlusher is the optional-behavior seam for the session-end memory
// write flush + failure report (ADR-068 §2.3, issue #1410 §4): only the
// plurHook concrete type implements both. TurnHook does not declare them;
// a nil/absent hook yields ok=false and no defer is registered.
type memoryFlusher interface {
	FlushSession(string)
	MemoryWriteReport(string) string
}

// Chat runs the multi-turn orchestration loop.
func (a *agent) Chat(ctx context.Context, s *ports.Session, prompt string) error {
	if err := a.ctxManager.AddContent(ctx, &domain_llm.Content{
		Role:  "user",
		Parts: []*domain_llm.Part{{Text: prompt}},
	}); err != nil {
		return fmt.Errorf("failed to initialize session history: %w", err)
	}

	// Memory: flush the per-session learning ring buffer at session end and
	// surface the write-failure aggregate — success and error (ADR-068 §2.3,
	// issue #1410 §4). Flush-then-read in ONE defer at the top of Chat:
	// LIFO-safe by construction — the report is read after the flush attempt,
	// so it includes the flush's own failure. The type assertion is the
	// optional-behavior seam: TurnHook does not declare these methods; only
	// the plurHook concrete type implements both. A nil/absent hook yields
	// ok=false and no defer is registered.
	if a.memoryHook != nil {
		if flusher, ok := a.memoryHook.(memoryFlusher); ok {
			// Primary surface is turns.log via the turns logger (best-effort:
			// this defer runs after the telemetry drain on ctx cancel, so it
			// may race); the synchronous stderr Warn is the asserted surface
			// (issue #1410 §4).
			defer func() {
				flusher.FlushSession(s.ID)
				if report := flusher.MemoryWriteReport(s.ID); report != "" {
					if a.turnsLogger != nil {
						a.turnsLogger.HandleEvent(context.Background(),
							events.SystemMessageEvent{Message: report, Level: "warn"})
					}
					a.getLogger().Warn("memory_write_failures", "report", report, "session", s.ID)
				}
			}()
		}
	}

	if err := a.applyConfig(ctx); err != nil {
		return err
	}
	a.emit(ctx, events.UserPromptEvent{Text: prompt})
	a.emit(ctx, events.StatusUpdate{Message: "Starting chat...", Level: "info"})

	// Register a hook that refreshes configuration between the inference
	// and execution phases of each turn. This enables mid-session hot-reload:
	// if the config file ModTime changes while the LLM is responding, the
	// updated limits (e.g. MAX_TURNS) take effect before tool execution.
	a.engine.ApplyOptions(orchestrator.WithEngineHook(&configRefreshHook{a: a}))

	// Coordinate the engine run loop and background telemetry workers using
	// sync.WaitGroup + errors.Join. Unlike errgroup (which returns only the first
	// non-nil error), this collects both errors so the caller can determine whether
	// the orchestration itself succeeded or failed.
	gCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		runErr       error
		telemetryErr error
		wg           sync.WaitGroup
	)

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer cancel() // Stop telemetry when Run finishes
		runErr = a.engine.Run(gCtx, s.StartTime, s.ID)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		telemetryErr = a.engine.StartTelemetry(gCtx)
	}()

	wg.Wait()
	return errors.Join(runErr, telemetryErr)
}

// configRefreshHook implements orchestrator.TurnHook to refresh the agent's
// runtime configuration when transitioning from the Inference phase to the
// Execution phase. This is the narrow window between receiving the LLM
// response and executing tool calls — the ideal point for mid-session
// config hot-reload because:
//   - The LLM response (which may include tool calls) is already in hand.
//   - The config file may have been modified while waiting for the LLM.
//   - Updated limits (MAX_TURNS, context window, etc.) take effect before
//     the Dispatcher evaluates whether to allow tool execution.
//
// Errors from applyConfig are intentionally ignored: Refresh is best-effort
// per ADR-029 §5, and the hook signature (TurnHook.BeforeTurn) does not
// return an error. A failed refresh leaves the prior config intact.
type configRefreshHook struct {
	a *agent
}

func (h *configRefreshHook) BeforeTurn(turn *orchestrator.Turn)           {}
func (h *configRefreshHook) AfterTurn(turn *orchestrator.Turn, err error) {}
func (h *configRefreshHook) OnPhaseTransition(from, to orchestrator.TurnPhase, state *orchestrator.TurnState) {
	if from == orchestrator.PhaseInference && to == orchestrator.PhaseExecuting {
		_ = h.a.applyConfig(context.Background())
	}
}

// Shutdown gracefully stops the agent and its components.
func (a *agent) Shutdown(ctx context.Context) error {
	var errs []error

	if a.turnsLogger != nil {
		if err := a.turnsLogger.Close(); err != nil {
			a.getLogger().Debug("turns logger shutdown incomplete", "error", err)
			errs = append(errs, err)
		}
	}

	if a.events != nil {
		if err := a.events.Flush(ctx); err != nil {
			if !errors.Is(err, events.ErrBusClosed) {
				a.getLogger().Debug("event bus flush incomplete during shutdown", "error", err)
				errs = append(errs, err)
			}
		}
		if err := a.events.Shutdown(ctx); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (a *agent) getLogger() ports.Logger {
	if a.logger != nil {
		return a.logger
	}
	return slog.Default()
}

// InternalAccessor provides access to internal agent components for the
// internal/agent/agentinternal sibling package, which wraps it with
// typed accessors and clearly-suffixed *ForTest mutators. Production
// code must not call any "*ForInternalUse" method. Production access
// to agent internals goes through ports.ChatterComposer or
// ports.Chatter. See ADR-022.
//
// The interface is satisfied only by the unexported *agent type.
// Callers obtain it via agent.AsInternal or agent.NewBareForInternalUse.
type InternalAccessor interface {
	ApplyConfig(ctx context.Context) error

	// AsChatter returns the underlying agent typed as ports.Chatter.
	// Used by agentinternal to pass the agent into production
	// functions that accept the ports.Chatter interface.
	AsChatter() ports.Chatter

	// Bridge methods consumed by agentinternal. Not for production use.
	GetTrackerForInternalUse() domain_pricing.CostTracker
	GetCtxManagerForInternalUse() *sessctx.Manager
	GetEventsForInternalUse() events.EventBus
	GetConfigWatcherForInternalUse() domain_config.ConfigWatcher
	GetRuntimeSnapshotForInternalUse() struct {
		ProviderName     string
		Model            string
		Mode             string
		PricingOverrides map[string]domain_pricing.ModelPricing
		Limits           events.Limits
		Memory           domain_config.MemoryConfig
	}
	SetEventsForInternalUse(bus events.EventBus)
	SetConfigWatcherForInternalUse(cw domain_config.ConfigWatcher)
	SetCtxManagerForInternalUse(cm *sessctx.Manager)
	SetLoggerForInternalUse(l ports.Logger)
	SetTrackerForInternalUse(t domain_pricing.CostTracker)
	SetRuntimeConfigForInternalUse(
		providerName, model, mode string,
		pricingOverrides map[string]domain_pricing.ModelPricing,
		limits events.Limits,
	)

	// DiffConfig compares the canonical intended configuration against
	// the live engine/context-manager state. Returns a DriftReport
	// describing any mismatches. Callers should check report.InSync
	// rather than testing for a nil report — DiffConfig never returns nil.
	DiffConfig() *DriftReport
}

// AsInternal wraps a ports.Chatter to provide access to its internal components.
// [FOR TESTING ONLY] This is a testing utility and should not be used in production code paths.
func AsInternal(c ports.Chatter) InternalAccessor {
	if a, ok := c.(*agent); ok {
		return a
	}
	return nil
}

func (a *agent) ApplyConfig(ctx context.Context) error {
	return a.applyConfig(ctx)
}
