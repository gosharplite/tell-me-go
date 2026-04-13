// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// Exported for external tests (Integration tests in tests/ directory)

// AgentInternal provides access to internal components of the agent for integration testing.
type AgentInternal struct {
	*agent
}

func (a *AgentInternal) ApplyConfig(ctx context.Context) error {
	return a.applyConfig(ctx)
}

func (a *AgentInternal) GetCtxManager() *session.ContextManager {
	return a.ctxManager
}

func (a *AgentInternal) GetEvents() events.EventBus {
	return a.events
}

func (a *AgentInternal) GetConfigWatcher() session.ConfigWatcher {
	return a.configWatcher
}

func (a *AgentInternal) SetTracker(t domain_pricing.CostTracker) {
	a.tracker = t
}

func (a *AgentInternal) GetTracker() domain_pricing.CostTracker {
	return a.tracker
}

func (a *AgentInternal) GetRuntimeConfig() any {
	return a.config.Load()
}

func (a *AgentInternal) SetConfigWatcher(cw session.ConfigWatcher) {
	a.configWatcher = cw
}

func (a *AgentInternal) SetEvents(bus events.EventBus) {
	a.events = bus
}

func (a *AgentInternal) SetLogger(l ports.Logger) {
	a.logger = l
}

func (a *AgentInternal) SetRuntimeConfig(cfg any) {
	a.config.Store(cfg.(*runtimeConfig))
}

// AsAgentInternal wraps a Chatter to provide access to internal components.
func AsAgentInternal(c ports.Chatter) *AgentInternal {
	if a, ok := c.(*agent); ok {
		return &AgentInternal{a}
	}
	return nil
}

func WithLoader(loader domain_config.ConfigLoader) Option {
	return withLoader(loader)
}

func WithSessionLoader(loader domain_config.SessionLoader) Option {
	return withSessionLoader(loader)
}

func WithInitContext(ctx context.Context) Option {
	return withInitContext(ctx)
}

func (a *AgentInternal) SetCtxManager(cm *session.ContextManager) {
	a.ctxManager = cm
}

// RuntimeConfigInternal exports runtimeConfig for integration tests.
type RuntimeConfigInternal = runtimeConfig

func NewAgentInternal() *AgentInternal {
	return &AgentInternal{agent: &agent{}}
}
