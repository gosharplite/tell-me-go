// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*agent)

// WithHistoryManager sets the history manager for the agent.
func WithHistoryManager(h ports.HistoryManager) AgentOption {
	return func(a *agent) {
		a.hManager = h
	}
}

// WithSecurityManager sets the security manager for the agent.
func WithSecurityManager(sm security.Manager) AgentOption {
	return func(a *agent) {
		a.sm = sm
	}
}

// WithSessionProvider sets the session provider for the agent.
func WithSessionProvider(sp ports.SessionProvider) AgentOption {
	return func(a *agent) {
		a.sessionProvider = sp
	}
}

// WithInitContext sets the context for the agent initialization.
func WithInitContext(ctx context.Context) AgentOption {
	return func(a *agent) {
		a.initCtx = ctx
	}
}

// WithSummarizer sets the summarizer service for the agent.
func WithSummarizer(s ports.Summarizer) AgentOption {
	return func(a *agent) {
		a.summarizer = s
	}
}

// WithInternalTools enables the registration of internal agent tools.
func WithInternalTools() AgentOption {
	return func(a *agent) {
		a.registerInternal = true
	}
}

// WithPricing sets the pricing configuration for cost estimation.
func WithPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) AgentOption {
	return func(a *agent) {
		a.model = model
		a.mode = mode
		a.pricingOverrides = overrides
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.CostTracker) AgentOption {
	return func(a *agent) {
		a.tracker = tracker
	}
}

// WithConfigWatcher sets the configuration watcher for hot-reload support.
func WithConfigWatcher(cw domain_config.ConfigWatcher) AgentOption {
	return func(a *agent) {
		a.configWatcher = cw
	}
}

// WithLogger sets the logger for the agent.
func WithLogger(l ports.Logger) AgentOption {
	return func(a *agent) {
		a.logger = l
	}
}

// WithSkillSelector sets the skill selector for the agent.
func WithSkillSelector(s skills.SkillSelector) AgentOption {
	return func(a *agent) {
		a.skillSelector = s
	}
}

// WithTurnsLogger sets the turns logger for the agent.
func WithTurnsLogger(tl ports.TurnsLogger) AgentOption {
	return func(a *agent) {
		a.turnsLogger = tl
	}
}

// WithClock sets the clock for the agent and its components.
func WithClock(c clock.Clock) AgentOption {
	return func(a *agent) {
		a.clock = c
	}
}

// WithProviderName sets the provider name for the agent.
func WithProviderName(name string) AgentOption {
	return func(a *agent) {
		a.providerName = name
	}
}

// WithSkillEcosystemIntro sets the ecosystem introduction text that is
// injected alongside skill content. The text is provided by the
// infrastructure layer and is opaque to the agent.
func WithSkillEcosystemIntro(intro string) AgentOption {
	return func(a *agent) {
		a.ecosystemIntro = intro
	}
}

// WithMemoryClient sets the MCP client for the MEMORY.SERVER and the seed
// MemoryConfig. A nil client is legal — it yields an inert memory
// integration (runtime nil-client guards fail open). The live config
// comes from the ConfigWatcher at runtime (later wiring task); seed only
// pre-populates the atomic before the first applyConfig.
func WithMemoryClient(client tools.MCPClient, seed domain_config.MemoryConfig) AgentOption {
	return func(a *agent) {
		a.memoryClient = client
		a.memorySeed = seed
	}
}
