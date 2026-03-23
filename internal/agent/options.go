// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// agentConfig holds initialization-only dependencies and configuration.
type agentConfig struct {
	summarizer       ports.Summarizer
	registerInternal bool
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	loader           domain_config.ConfigLoader
	sessionLoader    domain_config.SessionLoader
	tracker          domain_pricing.CostTracker
	initCtx          context.Context
	logger           *slog.Logger
}

// option defines a functional option for configuring an Agent.
type option func(*agentConfig)

// withInitContext sets the context for the agent initialization.
func withInitContext(ctx context.Context) option {
	return func(c *agentConfig) {
		c.initCtx = ctx
	}
}

// withSummarizer sets the summarizer service for the agent.
func withSummarizer(s ports.Summarizer) option {
	return func(c *agentConfig) {
		c.summarizer = s
	}
}

// withInternalTools enables the registration of internal agent tools.
func withInternalTools() option {
	return func(c *agentConfig) {
		c.registerInternal = true
	}
}

// withPricing sets the pricing configuration for cost estimation.
func withPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) option {
	return func(c *agentConfig) {
		c.model = model
		c.mode = mode
		c.pricingOverrides = overrides
	}
}

// withLoader sets the configuration loader for the agent.
func withLoader(loader domain_config.ConfigLoader) option {
	return func(c *agentConfig) {
		c.loader = loader
	}
}

// withSessionCostTracker sets the cost tracker for the agent.
func withSessionCostTracker(tracker domain_pricing.CostTracker) option {
	return func(c *agentConfig) {
		c.tracker = tracker
	}
}

// withSessionLoader sets the session configuration loader for the agent.
func withSessionLoader(loader domain_config.SessionLoader) option {
	return func(c *agentConfig) {
		c.sessionLoader = loader
	}
}
