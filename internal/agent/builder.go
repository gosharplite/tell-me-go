// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// AgentBuilder constructs *agent values for tests using composition
// instead of post-construction mutation. It exists to eliminate the need for
// Set*/Get* accessors on the production agent type.
//
// Usage:
//
//	a := agent.NewAgentBuilder(t).
//	    WithGateway(mockGateway).
//	    WithEventBus(mockBus).
//	    WithRegistry(mockRegistry).
//	    WithSecurityManager(sm).
//	    WithHistoryManager(hm).
//	    Build()
type AgentBuilder struct {
	t       testing.TB
	gateway llm.LLMGateway
	bus     events.EventBus
	reg     tools.Registry
	opts    []AgentOption
}

// NewAgentBuilder returns a builder bound to the given test. Build() will call
// t.Fatal on any construction error.
func NewAgentBuilder(t testing.TB) *AgentBuilder {
	t.Helper()
	return &AgentBuilder{t: t}
}

// --- Required dependencies (passed as positional args to NewAgent) ---

// WithGateway sets the LLM gateway for the agent. Required.
func (b *AgentBuilder) WithGateway(gw llm.LLMGateway) *AgentBuilder {
	b.gateway = gw
	return b
}

// WithEventBus sets the event bus for the agent. Required.
func (b *AgentBuilder) WithEventBus(bus events.EventBus) *AgentBuilder {
	b.bus = bus
	return b
}

// WithRegistry sets the tool registry for the agent. Required.
func (b *AgentBuilder) WithRegistry(reg tools.Registry) *AgentBuilder {
	b.reg = reg
	return b
}

// --- Optional dependencies ---

// WithSecurityManager sets the security manager for the agent.
func (b *AgentBuilder) WithSecurityManager(sm security.Manager) *AgentBuilder {
	b.opts = append(b.opts, WithSecurityManager(sm))
	return b
}

// WithHistoryManager sets the history manager for the agent.
func (b *AgentBuilder) WithHistoryManager(hm ports.HistoryManager) *AgentBuilder {
	b.opts = append(b.opts, WithHistoryManager(hm))
	return b
}

// WithSessionProvider sets the session provider for the agent.
func (b *AgentBuilder) WithSessionProvider(sp ports.SessionProvider) *AgentBuilder {
	b.opts = append(b.opts, WithSessionProvider(sp))
	return b
}

// WithInitContext sets the context for the agent initialization.
func (b *AgentBuilder) WithInitContext(ctx context.Context) *AgentBuilder {
	b.opts = append(b.opts, WithInitContext(ctx))
	return b
}

// WithSummarizer sets the summarizer service for the agent.
func (b *AgentBuilder) WithSummarizer(s ports.Summarizer) *AgentBuilder {
	b.opts = append(b.opts, WithSummarizer(s))
	return b
}

// WithInternalTools enables registration of internal agent tools.
func (b *AgentBuilder) WithInternalTools() *AgentBuilder {
	b.opts = append(b.opts, WithInternalTools())
	return b
}

// WithPricing sets the pricing configuration for cost estimation.
func (b *AgentBuilder) WithPricing(model, mode string, overrides map[string]domain_pricing.ModelPricing) *AgentBuilder {
	b.opts = append(b.opts, WithPricing(model, mode, overrides))
	return b
}

// WithLoader sets the configuration loader for the agent.
func (b *AgentBuilder) WithLoader(loader domain_config.ConfigLoader) *AgentBuilder {
	b.opts = append(b.opts, WithLoader(loader))
	return b
}

// WithSessionLoader sets the session configuration loader for the agent.
func (b *AgentBuilder) WithSessionLoader(loader domain_config.SessionLoader) *AgentBuilder {
	b.opts = append(b.opts, WithSessionLoader(loader))
	return b
}

// WithSkillSelector sets the skill selector for the agent.
func (b *AgentBuilder) WithSkillSelector(s skills.SkillSelector) *AgentBuilder {
	b.opts = append(b.opts, WithSkillSelector(s))
	return b
}

// WithTurnsLogger sets the turns logger for the agent.
func (b *AgentBuilder) WithTurnsLogger(tl ports.TurnsLogger) *AgentBuilder {
	b.opts = append(b.opts, WithTurnsLogger(tl))
	return b
}

// WithClock sets the clock for the agent and its components.
func (b *AgentBuilder) WithClock(c clock.Clock) *AgentBuilder {
	b.opts = append(b.opts, WithClock(c))
	return b
}

// WithProviderName sets the provider name for the agent.
func (b *AgentBuilder) WithProviderName(name string) *AgentBuilder {
	b.opts = append(b.opts, WithProviderName(name))
	return b
}

// WithLogger sets the logger for the agent.
func (b *AgentBuilder) WithLogger(l ports.Logger) *AgentBuilder {
	b.opts = append(b.opts, WithLogger(l))
	return b
}

// WithTracker sets the cost tracker for the agent.
func (b *AgentBuilder) WithTracker(tr domain_pricing.CostTracker) *AgentBuilder {
	b.opts = append(b.opts, WithSessionCostTracker(tr))
	return b
}

// WithConfigWatcher sets the config watcher for the agent.
func (b *AgentBuilder) WithConfigWatcher(cw session.ConfigWatcher) *AgentBuilder {
	b.opts = append(b.opts, WithConfigWatcher(cw))
	return b
}

// WithCtxManager sets the context manager for the agent.
func (b *AgentBuilder) WithCtxManager(cm *session.ContextManager) *AgentBuilder {
	b.opts = append(b.opts, WithCtxManager(cm))
	return b
}

// WithRuntimeConfig sets the runtime configuration for the agent.
func (b *AgentBuilder) WithRuntimeConfig(cfg *RuntimeConfigInternal) *AgentBuilder {
	b.opts = append(b.opts, WithRuntimeConfig(cfg))
	return b
}

// Build constructs the agent and fails the test on error.
func (b *AgentBuilder) Build() ports.Chatter {
	b.t.Helper()
	if b.gateway == nil {
		b.t.Fatal("agent: AgentBuilder requires WithGateway")
	}
	if b.bus == nil {
		b.t.Fatal("agent: AgentBuilder requires WithEventBus")
	}
	if b.reg == nil {
		b.t.Fatal("agent: AgentBuilder requires WithRegistry")
	}
	a, err := NewAgent(b.gateway, b.bus, b.reg, b.opts...)
	if err != nil {
		b.t.Fatalf("agent: AgentBuilder.Build: %v", err)
	}
	return a
}
