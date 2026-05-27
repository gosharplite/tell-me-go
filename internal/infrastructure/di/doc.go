// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

/*
Package di is the single composition root for the application.

It is the **only** place where concrete infrastructure implementations
are bound to domain interfaces. No other package in the codebase performs
dependency injection or adapter wiring — all wiring flows through
Bootstrapper and its eight sub-factories.

# Architecture

di lives in internal/infrastructure/ but has the unique privilege of
depending on both domain/ (interfaces) and infrastructure/ (implementations).
This is by design: it bridges the Clean Architecture layers, translating
dependency-inversion boundaries into concrete object graphs. See the
package dependency graph (make verify-architecture) for the full set of
imports this package is allowed to have.

# Entry Points

  - Bootstrapper             — primary façade; owns all sub-factories and
    exposes BuildSessionDependencies.
  - BootstrapperConfig       — pure value object holding factories and
    primitives needed to construct a Bootstrapper.
  - DefaultBootstrapperConfig — returns a BootstrapperConfig populated with
    canonical production defaults.
  - ConfigurableSecurityManager — extends the domain security.Manager with
    configuration methods consumed by sub-factories.

# Factory Delegation Pattern

Bootstrapper does not build components directly. It delegates to eight
unexported sub-factories, each responsible for one concern:

	sessionFactory      — session state, directory scaffolding, security setup,
	                      backup rotation (→ ports.SessionProvider, persistence.Paths)
	toolchainFactory    — tool registry, binary health checks, tool registration
	                      pipeline (→ tools.Registry, ports.HealthChecker)
	telemetryFactory    — pricing data, cost tracking, turns logging
	                      (→ pricing.PricingData, pricing.CostTracker, ports.TurnsLogger)
	historyFactory      — history persistence, archive reading, unified queries
	                      (→ ports.HistoryManager, ports.UnifiedHistoryProvider)
	healthFactory       — system health checks for persistence, LLM, and toolchain
	                      (→ ports.HealthCheckManager)
	uiFactory           — terminal rendering, history browsing TUI
	                      (→ ports.UIRenderer, ports.HistoryBrowser)
	chatFactory         — agent session lifecycle, Chatter construction
	                      (→ ports.ChatterFactory, agent.ChatService)
	suggestionFactory   — command suggestions from global prompt history
	                      (→ ports.SuggestionService)

All sub-factories are interface-typed within the di package (private
interfaces). The concrete default*Factory structs are constructed in
NewBootstrapper and never escape the package.

# Wiring Flow

NewBootstrapper(cfg)

	│
	├─► sessionFactory    ← homeDir, fs, sm, stdout, stderr, logger, rotate, newState
	├─► toolchainFactory  ← homeDir, fs, sm, workspacePolicy, registerAll, registerMetrics
	├─► telemetryFactory  ← homeDir, fs, sm, logger
	├─► historyFactory    ← homeDir, fs
	├─► healthFactory     ← (stateless; delegates to toolchainFactory at build time)
	├─► uiFactory         ← sm, stdout, stderr, logger
	├─► chatFactory       ← bootstrapper itself (as SessionLifecycleManager)
	└─► suggestionFactory ← homeDir, fs, stderr, logger, workspacePolicy

BuildSessionDependencies(ctx, cfg, ...)

	│
	├─► sessionFactory.BuildSession()        → sessionProvider, paths, cleanup
	├─► historyFactory.BuildHistoryManager() → hManager
	├─► telemetryFactory.BuildTelemetry()    → pricingData, tracker, turnsLogger
	├─► lazyClient (sync.Once)              → llm.ExtendedClient (on first call)
	├─► healthFactory.BuildHealthManager()   → health (wired into sessionDeps)
	└─► lazyRegistry (sync.Once)            → tools.Registry (on first call)

The result is sessionDeps, which implements ports.SessionDependencies —
the single interface consumed by agent.Chatter implementations.

# Lazy Initialization

Two components use sync.Once-guarded lazy initialization to avoid
premature work:

	lazyClient (lazy_client.go)
	  Wraps the LLM client factory. The underlying provider client
	  (Anthropic, OpenAI, Gemini) is not created until the first
	  Generate/SendChat call. This avoids credential prompts and
	  authentication at startup when the LLM is not yet needed.

	lazyRegistry (lazy_registry.go)
	  Wraps the tool registry factory. Tool registration involves
	  file-system scanning, binary discovery, and security policy
	  evaluation. Deferring this to first use avoids unnecessary
	  work when the registry is never accessed (e.g., health checks,
	  history browsing).

Both types implement their respective domain interfaces directly and
are consumed through the sessionDeps accessor methods:
GetGateway() and GetRegistry().

# Maintainer Guidance

Adding a new dependency:
 1. Define the domain interface in internal/domain/ports/ if it does
    not already exist.
 2. Create a new sub-factory file (e.g., foo_factory.go) with a private
    fooFactory interface and a defaultFooFactory struct.
 3. Add the sub-factory field to Bootstrapper and construct it in
    NewBootstrapper.
 4. Wire the dependency into BuildSessionDependencies, attaching it
    to sessionDeps if it belongs to the session lifecycle, or exposing
    it via a new public accessor on Bootstrapper if it is a standalone
    service (see GetSuggestionService, GetHistoryBrowser for examples).

Test doubles:

	Every factory function in BootstrapperConfig has a production default
	set by DefaultBootstrapperConfig(). Tests inject doubles by constructing
	a custom BootstrapperConfig with mock factories. This is the canonical
	(and only) seam for injecting test doubles into the application graph.

See: https://github.com/gosharplite/tell-me-go/issues/598
*/
package di
