// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// AgentConstructor mirrors the signature of agent.NewAgent. It accepts
// opaque client and registry values plus variadic options so that the
// builder can remain decoupled from internal/agent (avoiding import cycles).
type AgentConstructor func(client any, bus events.EventBus, registry any, opts ...any) (ports.Chatter, error)

// AgentTestSetters provides the mutation operations that export_test.go
// exposes. The caller (test code in package agent or agent_test) injects a
// concrete implementation that wraps the export_test.go setters.
//
// Session-scoped parameters (ctxManager, configWatcher) use any to avoid
// importing internal/agent/session, which would create an import cycle.
type AgentTestSetters interface {
	SetEvents(c ports.Chatter, e events.EventBus)
	SetCtxManager(c ports.Chatter, cm any)
	SetConfigWatcher(c ports.Chatter, cw any)
	SetTracker(c ports.Chatter, t domain_pricing.CostTracker)
	SetLogger(c ports.Chatter, l ports.Logger)
	SetRuntimeConfig(c ports.Chatter, rc any)
}

// AgentBuilder provides a fluent API for constructing an agent with
// custom overrides suitable for integration tests.
//
// The builder lives in agenttest and uses Dependency Injection to avoid
// importing internal/agent. The constructor (mirroring agent.NewAgent)
// and setters (wrapping export_test.go) are injected by the caller.
type AgentBuilder struct {
	t     *testing.T
	ctor  AgentConstructor
	setts AgentTestSetters

	// Opaque dependencies forwarded to the constructor.
	client   any
	registry any
	opts     []any

	events        events.EventBus
	ctxManager    any
	configWatcher any
	tracker       domain_pricing.CostTracker
	logger        ports.Logger
	runtimeCfg    any
}

// NewAgentBuilder creates a new AgentBuilder.
//
// ctor should be agent.NewAgent (or a compatible wrapper). setts should
// wrap the export_test.go setters (e.g. agent.SetEventsForTest). client
// and registry are the LLM gateway and tool registry respectively; opts
// are functional options (agent.WithSecurityManager, etc.).
func NewAgentBuilder(t *testing.T, ctor AgentConstructor, client any, registry any, setts AgentTestSetters, opts ...any) *AgentBuilder {
	return &AgentBuilder{
		t:        t,
		ctor:     ctor,
		client:   client,
		registry: registry,
		setts:    setts,
		opts:     opts,
	}
}

// WithEvents sets the EventBus override.
func (b *AgentBuilder) WithEvents(e events.EventBus) *AgentBuilder {
	b.events = e
	return b
}

// WithCtxManager sets the ContextManager override.
func (b *AgentBuilder) WithCtxManager(cm any) *AgentBuilder {
	b.ctxManager = cm
	return b
}

// WithConfigWatcher sets the ConfigWatcher override.
func (b *AgentBuilder) WithConfigWatcher(cw any) *AgentBuilder {
	b.configWatcher = cw
	return b
}

// WithTracker sets the CostTracker override.
func (b *AgentBuilder) WithTracker(t domain_pricing.CostTracker) *AgentBuilder {
	b.tracker = t
	return b
}

// WithLogger sets the Logger override.
func (b *AgentBuilder) WithLogger(l ports.Logger) *AgentBuilder {
	b.logger = l
	return b
}

// WithRuntimeConfig sets the RuntimeConfig override.
func (b *AgentBuilder) WithRuntimeConfig(rc any) *AgentBuilder {
	b.runtimeCfg = rc
	return b
}

// Build constructs the agent with the configured overrides.
func (b *AgentBuilder) Build() ports.Chatter {
	b.t.Helper()

	bus := b.events
	if bus == nil {
		bus = events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	}

	chatter, err := b.ctor(b.client, bus, b.registry, b.opts...)
	if err != nil {
		b.t.Fatalf("AgentBuilder.Build: constructor failed: %v", err)
	}

	if b.ctxManager != nil {
		b.setts.SetCtxManager(chatter, b.ctxManager)
	}
	if b.configWatcher != nil {
		b.setts.SetConfigWatcher(chatter, b.configWatcher)
	}
	if b.tracker != nil {
		b.setts.SetTracker(chatter, b.tracker)
	}
	if b.logger != nil {
		b.setts.SetLogger(chatter, b.logger)
	}
	if b.runtimeCfg != nil {
		b.setts.SetRuntimeConfig(chatter, b.runtimeCfg)
	}

	return chatter
}

// stubSecurityManager is a minimal security.Manager that allows
// everything. It is exported so that callers can wrap it into an
// agent.WithSecurityManager option.
type stubSecurityManager struct {
	domain_security.Manager
	allowAll bool
}

func (s *stubSecurityManager) IsPathSafe(path string) (string, error) { return path, nil }
func (s *stubSecurityManager) IsPathWritable(path string) (string, error) {
	return path, nil
}
func (s *stubSecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	return true, nil
}
func (s *stubSecurityManager) LogAudit(action string, args ...any) {}
func (s *stubSecurityManager) Close() error                        { return nil }
func (s *stubSecurityManager) TerminalLock()                       {}
func (s *stubSecurityManager) TerminalUnlock()                     {}
func (s *stubSecurityManager) Prompt(message string)               {}
func (s *stubSecurityManager) Warn(message string)                 {}
func (s *stubSecurityManager) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (s *stubSecurityManager) ReadLine(ctx context.Context) (string, error) { return "", nil }
func (s *stubSecurityManager) IsCommandAllowed(command string) bool {
	return s.allowAll
}
func (s *stubSecurityManager) IsBypassActive() bool { return false }

var _ domain_security.Manager = (*stubSecurityManager)(nil)
