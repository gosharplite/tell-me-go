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
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// AgentOption defines a functional option for configuring an Agent.
type AgentOption func(*Agent)

// WithHistoryManager sets the history manager for the agent.
func WithHistoryManager(h ports.HistoryManager) AgentOption {
	return func(a *Agent) {
		a.hManager = h
	}
}

// WithSecurityManager sets the security manager for the agent.
func WithSecurityManager(sm security.Manager) AgentOption {
	return func(a *Agent) {
		a.sm = sm
	}
}

// WithSessionProvider sets the session provider for the agent.
func WithSessionProvider(sp ports.SessionProvider) AgentOption {
	return func(a *Agent) {
		a.sessionProvider = sp
	}
}

// WithInitContext sets the context for the agent initialization.
func WithInitContext(ctx context.Context) AgentOption {
	return func(a *Agent) {
		a.initCtx = ctx
	}
}

// WithSummarizer sets the summarizer service for the agent.
func WithSummarizer(s ports.Summarizer) AgentOption {
	return func(a *Agent) {
		a.summarizer = s
	}
}

// WithInternalTools enables the registration of internal agent tools.
func WithInternalTools() AgentOption {
	return func(a *Agent) {
		a.registerInternal = true
	}
}

// WithPricing sets the pricing configuration for cost estimation.
func WithPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) AgentOption {
	return func(a *Agent) {
		a.model = model
		a.mode = mode
		a.pricingOverrides = overrides
	}
}

// WithLoader sets the configuration loader for the agent.
func WithLoader(loader domain_config.ConfigLoader) AgentOption {
	return func(a *Agent) {
		a.loader = loader
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.CostTracker) AgentOption {
	return func(a *Agent) {
		a.tracker = tracker
	}
}

// WithSessionLoader sets the session configuration loader for the agent.
func WithSessionLoader(loader domain_config.SessionLoader) AgentOption {
	return func(a *Agent) {
		a.sessionLoader = loader
	}
}

// WithLogger sets the logger for the agent.
func WithLogger(l ports.Logger) AgentOption {
	return func(a *Agent) {
		a.logger = l
	}
}

// WithSkillSelector sets the skill selector for the agent.
func WithSkillSelector(s skills.SkillSelector) AgentOption {
	return func(a *Agent) {
		a.skillSelector = s
	}
}

// WithTurnsLogger sets the turns logger for the agent.
func WithTurnsLogger(tl ports.TurnsLogger) AgentOption {
	return func(a *Agent) {
		a.turnsLogger = tl
	}
}

// WithClock sets the clock for the agent and its components.
func WithClock(c clock.Clock) AgentOption {
	return func(a *Agent) {
		a.clock = c
	}
}

// WithProviderName sets the provider name for the agent.
func WithProviderName(name string) AgentOption {
	return func(a *Agent) {
		a.providerName = name
	}
}
