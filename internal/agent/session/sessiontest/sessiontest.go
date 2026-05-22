// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package sessiontest

import (
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	domaintools "github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Deps holds the required components for a session.
type Deps struct {
	Paths            *persistence.Paths
	HistoryManager   ports.HistoryManager
	Client           domain_llm.LLMClient
	Gateway          domain_llm.LLMGateway
	Registry         domaintools.Registry
	SecurityManager  domain_security.Manager
	Tracker          domain_pricing.CostTracker
	PricingData      domain_pricing.PricingData
	PricingOverrides map[string]domain_pricing.ModelPricing
	EventBus         events.EventBus
	Logger           ports.Logger
	TurnsLogger      ports.TurnsLogger
	SessionProvider  ports.SessionProvider
	Health           ports.HealthCheckManager
}

// New creates a new Deps with all required components.
func New(paths *persistence.Paths, hManager ports.HistoryManager, client domain_llm.LLMClient, gw domain_llm.LLMGateway, reg domaintools.Registry, sm domain_security.Manager, tracker domain_pricing.CostTracker, pData domain_pricing.PricingData, overrides map[string]domain_pricing.ModelPricing, bus events.EventBus, logger ports.Logger, turnsLogger ports.TurnsLogger, sessionProvider ports.SessionProvider, health ports.HealthCheckManager) ports.SessionDependencies {
	return &Deps{
		Paths:            paths,
		HistoryManager:   hManager,
		Client:           client,
		Gateway:          gw,
		Registry:         reg,
		SecurityManager:  sm,
		Tracker:          tracker,
		PricingData:      pData,
		PricingOverrides: overrides,
		EventBus:         bus,
		Logger:           logger,
		TurnsLogger:      turnsLogger,
		SessionProvider:  sessionProvider,
		Health:           health,
	}
}

func (d *Deps) GetGateway() domain_llm.LLMGateway { return d.Gateway }

func (d *Deps) GetHistoryManager() ports.HistoryManager {
	return d.HistoryManager
}

func (d *Deps) GetRegistry() (domaintools.Registry, error) { return d.Registry, nil }

func (d *Deps) GetSecurityManager() domain_security.Manager {
	return d.SecurityManager
}

func (d *Deps) GetEventBus() events.EventBus { return d.EventBus }

func (d *Deps) GetLogger() ports.Logger { return d.Logger }

func (d *Deps) GetTurnsLogger() ports.TurnsLogger {
	return d.TurnsLogger
}

func (d *Deps) GetPaths() *persistence.Paths { return d.Paths }

func (d *Deps) GetSessionProvider() ports.SessionProvider {
	return d.SessionProvider
}

func (d *Deps) GetPricingOverrides() map[string]domain_pricing.ModelPricing {
	return d.PricingOverrides
}

func (d *Deps) GetTracker() domain_pricing.CostTracker { return d.Tracker }

func (d *Deps) GetPricingData() domain_pricing.PricingData {
	return d.PricingData
}

func (d *Deps) GetHealthManager() ports.HealthCheckManager { return d.Health }
