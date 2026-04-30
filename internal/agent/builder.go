// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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

// --- Optional dependencies (one per Set* method being removed) ---

// WithLogger sets the logger for the agent.
func (b *AgentBuilder) WithLogger(l ports.Logger) *AgentBuilder {
	b.opts = append(b.opts, WithLogger(l))
	return b
}

// WithTracker sets the cost tracker for the agent.
func (b *AgentBuilder) WithTracker(tr pricing.CostTracker) *AgentBuilder {
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
