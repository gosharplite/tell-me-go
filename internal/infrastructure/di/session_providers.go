// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// infraProvider holds core plumbing dependencies: filesystem paths,
// security manager, event bus, and loggers.
type infraProvider struct {
	paths       *persistence.Paths
	sm          security.Manager
	bus         events.EventBus
	logger      ports.Logger
	turnsLogger ports.TurnsLogger
}

// GetPaths returns the persistence paths.
func (p *infraProvider) GetPaths() *persistence.Paths { return p.paths }

// GetSecurityManager returns the security manager.
func (p *infraProvider) GetSecurityManager() security.Manager { return p.sm }

// GetEventBus returns the event bus.
func (p *infraProvider) GetEventBus() events.EventBus { return p.bus }

// GetLogger returns the logger.
func (p *infraProvider) GetLogger() ports.Logger { return p.logger }

// GetTurnsLogger returns the turns logger.
func (p *infraProvider) GetTurnsLogger() ports.TurnsLogger { return p.turnsLogger }

// telemetryProvider holds pricing and cost-tracking dependencies.
type telemetryProvider struct {
	tracker          pricing.CostTracker
	pricingOverrides map[string]pricing.ModelPricing
}

// GetTracker returns the cost tracker.
func (p *telemetryProvider) GetTracker() pricing.CostTracker { return p.tracker }

// GetPricingOverrides returns the pricing overrides.
func (p *telemetryProvider) GetPricingOverrides() map[string]pricing.ModelPricing {
	return p.pricingOverrides
}

// sessionStateProvider holds session-level state: history, session
// provider, and workspace policy.
type sessionStateProvider struct {
	hManager        ports.HistoryManager
	sessionProvider ports.SessionProvider
	workspacePolicy services.WorkspacePolicy
}

// GetHistoryManager returns the history manager.
func (p *sessionStateProvider) GetHistoryManager() ports.HistoryManager { return p.hManager }

// GetSessionProvider returns the session provider.
func (p *sessionStateProvider) GetSessionProvider() ports.SessionProvider { return p.sessionProvider }

// GetWorkspacePolicy returns the workspace policy.
func (p *sessionStateProvider) GetWorkspacePolicy() services.WorkspacePolicy {
	return p.workspacePolicy
}

// lazyProvider holds lazily-initialized components: the LLM client
// and the tool registry.
type lazyProvider struct {
	client   *lazyClient
	registry *lazyRegistry
}

// GetGateway returns the lazily-initialized LLM gateway.
func (p *lazyProvider) GetGateway() llm.LLMGateway { return p.client }

// GetClient returns the lazily-initialized LLM client.
func (p *lazyProvider) GetClient() llm.LLMClient { return p.client }

// GetRegistry returns the lazily-initialized tool registry.
func (p *lazyProvider) GetRegistry() (tools.Registry, error) { return p.registry.get() }

// healthProvider holds the health check manager.
type healthProvider struct {
	health ports.HealthCheckManager
}

// GetHealthManager returns the health check manager.
func (p *healthProvider) GetHealthManager() ports.HealthCheckManager { return p.health }
