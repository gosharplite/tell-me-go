// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
)

// agentConfig holds initialization-only dependencies and configuration.
type agentConfig struct {
	summarizer       ports.Summarizer
	sessionProvider  ports.SessionProvider
	skillSelector    skills.SkillSelector
	registerInternal bool
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	loader           domain_config.ConfigLoader
	sessionLoader    domain_config.SessionLoader
	tracker          domain_pricing.CostTracker
	initCtx          context.Context
	logger           *slog.Logger
	turnsLogger      ports.TurnsLogger
}

// Option defines a functional option for configuring an Agent.
type Option func(*agentConfig)

// WithSessionProvider sets the session provider for the agent.
func WithSessionProvider(sp ports.SessionProvider) Option {
	return func(c *agentConfig) {
		c.sessionProvider = sp
	}
}

// withInitContext sets the context for the agent initialization.
func withInitContext(ctx context.Context) Option {
	return func(c *agentConfig) {
		c.initCtx = ctx
	}
}

// WithSummarizer sets the summarizer service for the agent.
func WithSummarizer(s ports.Summarizer) Option {
	return func(c *agentConfig) {
		c.summarizer = s
	}
}

// WithInternalTools enables the registration of internal agent tools.
func WithInternalTools() Option {
	return func(c *agentConfig) {
		c.registerInternal = true
	}
}

// WithPricing sets the pricing configuration for cost estimation.
func WithPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) Option {
	return func(c *agentConfig) {
		c.model = model
		c.mode = mode
		c.pricingOverrides = overrides
	}
}

// withLoader sets the configuration loader for the agent.
func withLoader(loader domain_config.ConfigLoader) Option {
	return func(c *agentConfig) {
		c.loader = loader
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.CostTracker) Option {
	return func(c *agentConfig) {
		c.tracker = tracker
	}
}

// withSessionLoader sets the session configuration loader for the agent.
func withSessionLoader(loader domain_config.SessionLoader) Option {
	return func(c *agentConfig) {
		c.sessionLoader = loader
	}
}

// WithLogger sets the logger for the agent.
func WithLogger(l *slog.Logger) Option {
	return func(c *agentConfig) {
		c.logger = l
	}
}

// WithSkillSelector sets the skill selector for the agent.
func WithSkillSelector(s skills.SkillSelector) Option {
	return func(c *agentConfig) {
		c.skillSelector = s
	}
}

// WithTurnsLogger sets the turns logger for the agent.
func WithTurnsLogger(tl ports.TurnsLogger) Option {
	return func(c *agentConfig) {
		c.turnsLogger = tl
	}
}
