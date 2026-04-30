// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agentinternal

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// AgentBuilder provides a fluent API for constructing an agent with
// custom overrides suitable for integration tests.
//
// The builder lives in agentinternal (not agenttest) because it must
// import internal/agent to call NewAgent and AsInternal. Placing it
// in agenttest would create an import cycle:
//
//	agent/*_test.go → agenttest → agent
//
// agentinternal is already a leaf package that imports agent, so
// adding the builder here is safe.
type AgentBuilder struct {
	t *testing.T

	events        events.EventBus
	ctxManager    *session.ContextManager
	configWatcher session.ConfigWatcher
	tracker       domain_pricing.CostTracker
	logger        ports.Logger
	runtimeCfg    *agent.RuntimeConfigInternal
}

// NewAgentBuilder creates a new AgentBuilder for the given test.
func NewAgentBuilder(t *testing.T) *AgentBuilder {
	return &AgentBuilder{t: t}
}

// WithEvents sets the EventBus override. When nil (default), the builder
// will create a minimal synchronous event bus.
func (b *AgentBuilder) WithEvents(e events.EventBus) *AgentBuilder {
	b.events = e
	return b
}

// WithCtxManager sets the ContextManager override.
func (b *AgentBuilder) WithCtxManager(cm *session.ContextManager) *AgentBuilder {
	b.ctxManager = cm
	return b
}

// WithConfigWatcher sets the ConfigWatcher override.
func (b *AgentBuilder) WithConfigWatcher(cw session.ConfigWatcher) *AgentBuilder {
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
func (b *AgentBuilder) WithRuntimeConfig(rc *agent.RuntimeConfigInternal) *AgentBuilder {
	b.runtimeCfg = rc
	return b
}

// Build constructs the agent with the configured overrides. It calls
// the standard NewAgent constructor with minimal valid defaults and then
// injects test-specific overrides through the export_test.go setters.
//
// The returned ports.Chatter can be further unwrapped via
// agent.AsInternal() if the caller needs to read internal state back.
func (b *AgentBuilder) Build() ports.Chatter {
	b.t.Helper()

	bus := b.events
	if bus == nil {
		bus = events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	}

	reg := agenttest.NewMockToolRegistry()
	gw := &agenttest.MockGateway{}
	sm := &stubSecurityManager{allowAll: true}

	chatter, err := agent.NewAgent(gw, bus, reg,
		agent.WithSecurityManager(sm),
	)
	if err != nil {
		b.t.Fatalf("AgentBuilder.Build: NewAgent failed: %v", err)
	}

	// Inject overrides via the export_test.go setters (avoiding InternalAccessor).
	if b.ctxManager != nil {
		agent.SetCtxManagerForTest(chatter, b.ctxManager)
	}
	if b.configWatcher != nil {
		agent.SetConfigWatcherForTest(chatter, b.configWatcher)
	}
	if b.tracker != nil {
		agent.SetTrackerForTest(chatter, b.tracker)
	}
	if b.logger != nil {
		agent.SetLoggerForTest(chatter, b.logger)
	}
	if b.runtimeCfg != nil {
		agent.SetRuntimeConfigForTest(chatter, b.runtimeCfg)
	}

	return chatter
}

// stubSecurityManager is a minimal security.Manager that allows
// everything. It is used by AgentBuilder to satisfy NewAgent's
// mandatory WithSecurityManager requirement without forcing callers
// to configure mock expectations.
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

// Compile-time interface check.
var _ domain_security.Manager = (*stubSecurityManager)(nil)
