// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// agentConfig holds initialization-only dependencies and configuration.
type agentConfig struct {
	registry         tools.IToolRegistry
	sm               domain_security.ISecurityManager
	summarizer       services.Summarizer
	hManager         services.HistoryManager
	registerInternal bool
	model            string
	mode             string
	pricingOverrides map[string]domain_pricing.ModelPricing
	loader           domain_config.ConfigLoader
	tracker          domain_pricing.ICostTracker
}

// Option defines a functional option for configuring an Agent.
type Option func(*agentConfig)

// WithRegistry sets the tool registry for the agent.
func WithRegistry(r tools.IToolRegistry) Option {
	return func(c *agentConfig) {
		c.registry = r
	}
}

// WithSecurityManager sets the security manager for the agent.
func WithSecurityManager(sm domain_security.ISecurityManager) Option {
	return func(c *agentConfig) {
		c.sm = sm
	}
}

// WithSummarizer sets the summarizer service for the agent.
func WithSummarizer(s services.Summarizer) Option {
	return func(c *agentConfig) {
		c.summarizer = s
	}
}

// WithHistoryManager sets the history manager for the agent.
func WithHistoryManager(h services.HistoryManager) Option {
	return func(c *agentConfig) {
		c.hManager = h
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

// WithLoader sets the configuration loader for the agent.
func WithLoader(loader domain_config.ConfigLoader) Option {
	return func(c *agentConfig) {
		c.loader = loader
	}
}

// WithSessionCostTracker sets the cost tracker for the agent.
func WithSessionCostTracker(tracker domain_pricing.ICostTracker) Option {
	return func(c *agentConfig) {
		c.tracker = tracker
	}
}
