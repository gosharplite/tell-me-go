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

// Export for external tests
type AgentInternal = agent
type RuntimeConfigInternal = runtimeConfig
type MockLLMClient = mockLLMClient
type MockHistoryManager = mockHistoryManager
type MockSummarizer = mockSummarizer
type MockCostTracker = mockTracker
type MockExecutor = mockExecutor
type MockSecurityManager = mockSecurityManager

func (a *agent) ApplyConfig(ctx context.Context) error {
	return a.applyConfig(ctx)
}

func (a *agent) GetCtxManager() *session.ContextManager {
	return a.ctxManager
}

func (a *agent) GetEvents() events.EventBus {
	return a.events
}

func (a *agent) GetConfigWatcher() session.ConfigWatcher {
	return a.configWatcher
}

func (a *agent) SetTracker(t domain_pricing.CostTracker) {
	a.tracker = t
}

func (a *agent) GetTracker() domain_pricing.CostTracker {
	return a.tracker
}

func (a *agent) GetRuntimeConfig() any {
	return a.config.Load()
}

func (a *agent) SetConfigWatcher(cw session.ConfigWatcher) {
	a.configWatcher = cw
}

func (a *agent) SetEvents(bus events.EventBus) {
	a.events = bus
}

func (a *agent) SetLogger(l ports.Logger) {
	a.logger = l
}

func (a *agent) SetRuntimeConfig(cfg any) {
	a.config.Store(cfg.(*runtimeConfig))
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

func (a *agent) SetCtxManager(cm *session.ContextManager) {
	a.ctxManager = cm
}
