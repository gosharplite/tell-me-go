// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agentinternal exposes test helpers that legitimately need to
// import the parent internal/agent package — typed accessors for its
// internal state, mocks of interfaces it owns whose method signatures
// reference internal/agent types, and a "bare agent" constructor used
// by tests that need to exercise narrow code paths without going
// through the full NewAgent() initialization.
//
// These helpers cannot live in internal/agent/agenttest because that
// package must remain a leaf with no upward dependency on
// internal/agent: any such dependency creates an import cycle for
// internal/agent's own internal-package _test.go files (those declared
// `package agent` rather than `package agent_test`).
//
// This package is itself a regular (non-test) package because Go's
// import rules forbid non-test code from importing _test packages. Its
// visibility is correctly restricted by Go's internal/ rules to the
// internal/agent/... subtree, plus the project's tests/ tree.
//
// See ADR-021 (test doubles in *test sub-packages) and ADR-022
// (test-only access via export_test.go and *internal sub-packages).
package agentinternal

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/mock"
)

// AgentInternal wraps an agent.InternalAccessor with typed, read-only
// accessors and a small set of clearly-suffixed "*ForTest" mutators
// used by white-box tests in this package's siblings (notably
// tests/integration/agent and the internal/agent _test files declared
// as package agent_test).
//
// The exported name (was `agentInternal`) is uppercase so that test
// helpers in other packages can declare typed parameters of this type.
type AgentInternal struct {
	raw agent.InternalAccessor
}

// AsAgentInternal wraps a ports.Chatter to provide access to its internal
// components. Returns nil if c is not the production *agent type.
func AsAgentInternal(c ports.Chatter) *AgentInternal {
	inner := agent.AsInternal(c)
	if inner == nil {
		return nil
	}
	return &AgentInternal{raw: inner}
}

// NewBareAgent constructs an uninitialized agent suitable for tests that
// need to exercise narrow code paths (e.g. a Shutdown error path) without
// going through the full NewAgent() initialization. Production code must
// never call this. See ADR-022.
func NewBareAgent() *AgentInternal {
	return &AgentInternal{raw: agent.NewBareForInternalUse()}
}

// Chatter returns the underlying ports.Chatter, suitable for passing to
// production methods that accept the public interface.
func (a *AgentInternal) Chatter() ports.Chatter {
	return a.raw.AsChatter()
}

// ---------------------------------------------------------------------
// Typed read-only accessors.
//
// These wrap unexported state on *agent so that tests can assert on
// post-construction state without the agent package having to expose
// raw getters on its public InternalAccessor interface.
// ---------------------------------------------------------------------

// GetCtxManager returns the agent's session.ContextManager.
func (a *AgentInternal) GetCtxManager() *session.ContextManager {
	return a.raw.GetCtxManagerForInternalUse()
}

// GetEvents returns the agent's events.EventBus.
func (a *AgentInternal) GetEvents() events.EventBus {
	return a.raw.GetEventsForInternalUse()
}

// GetConfigWatcher returns the agent's session.ConfigWatcher.
func (a *AgentInternal) GetConfigWatcher() session.ConfigWatcher {
	return a.raw.GetConfigWatcherForInternalUse()
}

// GetTracker returns the agent's domain_pricing.CostTracker.
//
// NOTE: GetTracker is the one accessor that also exists on the public
// agent.InternalAccessor interface, because production callers in
// infrastructure/factory/chatter.go and infrastructure/di/container.go
// rely on it. See ADR-022 for the recorded exception and #87 for the
// follow-up to remove the production callers.
func (a *AgentInternal) GetTracker() domain_pricing.CostTracker {
	return a.raw.GetTracker()
}

// RuntimeSnapshot is a stable, value-typed view of the agent's
// internal runtime configuration, returned by GetRuntimeConfig.
//
// It exists so that tests can assert on configuration fields
// (Model, Mode, Limits, PricingOverrides) without the agent package
// having to export its internal runtimeConfig type alias.
type RuntimeSnapshot struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	Limits           events.Limits
}

// GetRuntimeConfig returns a snapshot of the agent's current runtime
// configuration. The returned value is a copy; mutating it does not
// affect the agent's state.
func (a *AgentInternal) GetRuntimeConfig() RuntimeSnapshot {
	return a.raw.GetRuntimeSnapshotForInternalUse()
}

// ApplyConfig forces a re-application of the agent's current runtime
// configuration. Tests use this to verify that mutated state propagates
// to the engine and context manager.
func (a *AgentInternal) ApplyConfig(ctx context.Context) error {
	return a.raw.ApplyConfig(ctx)
}

// Chat invokes the underlying agent's Chat method. Provided for
// convenience so test code holding an *AgentInternal does not have to
// call .Chatter().Chat(...) explicitly.
func (a *AgentInternal) Chat(ctx context.Context, sess *ports.Session, prompt string) error {
	return a.raw.AsChatter().Chat(ctx, sess, prompt)
}

// Shutdown invokes the underlying agent's Shutdown method.
func (a *AgentInternal) Shutdown(ctx context.Context) error {
	return a.raw.AsChatter().Shutdown(ctx)
}

// ---------------------------------------------------------------------
// Test-only mutators.
//
// These are clearly suffixed "*ForTest" to brand them as
// non-production at every call site. They are the only legitimate
// path through which test code may mutate agent state mid-test.
// ---------------------------------------------------------------------

// SetEventsForTest replaces the agent's events.EventBus.
func (a *AgentInternal) SetEventsForTest(bus events.EventBus) {
	a.raw.SetEventsForInternalUse(bus)
}

// SetConfigWatcherForTest replaces the agent's session.ConfigWatcher.
func (a *AgentInternal) SetConfigWatcherForTest(cw session.ConfigWatcher) {
	a.raw.SetConfigWatcherForInternalUse(cw)
}

// SetCtxManagerForTest replaces the agent's session.ContextManager.
func (a *AgentInternal) SetCtxManagerForTest(cm *session.ContextManager) {
	a.raw.SetCtxManagerForInternalUse(cm)
}

// SetLoggerForTest replaces the agent's ports.Logger.
func (a *AgentInternal) SetLoggerForTest(l ports.Logger) {
	a.raw.SetLoggerForInternalUse(l)
}

// SetTrackerForTest replaces the agent's domain_pricing.CostTracker.
//
// NOTE: production code mutates the tracker only via the
// WithSessionCostTracker option at construction. This setter exists
// solely to support tests that verify reconfiguration behavior. See
// ADR-022.
func (a *AgentInternal) SetTrackerForTest(t domain_pricing.CostTracker) {
	a.raw.SetTrackerForInternalUse(t)
}

// SetRuntimeConfigForTest replaces the agent's internal runtime
// configuration. Used by tests that exercise applyConfig/Publish
// error paths and need a non-nil but deliberately bare config.
//
// Passing a zero RuntimeSnapshot is the typical use; it produces an
// internal runtimeConfig with all zero-valued fields, which is enough
// to satisfy applyConfig's nil check before publishing the
// ConfigUpdated event.
func (a *AgentInternal) SetRuntimeConfigForTest(snap RuntimeSnapshot) {
	a.raw.SetRuntimeConfigForInternalUse(snap.ProviderName, snap.Model, snap.Mode, snap.PricingOverrides, snap.Limits)
}

// ---------------------------------------------------------------------
// Mocks (unchanged from the previous incarnation of this file).
// ---------------------------------------------------------------------

// mockSessionLifecycleManager is a mock of agent.SessionLifecycleManager.
// It must live in this package (rather than in agenttest) because its
// BuildSessionDependencies signature references agent.CapturerInteractor,
// which is a distinct interface declared in internal/agent.
type mockSessionLifecycleManager struct {
	mock.Mock
}

func (m *mockSessionLifecycleManager) BuildSessionDependencies(ctx context.Context, cfg *config.Config, configPath string, newSession bool, capturer agent.CapturerInteractor) (ports.SessionDependencies, ports.HistoryManager, func(context.Context) error, error) {
	args := m.Called(ctx, cfg, configPath, newSession, capturer)
	var deps ports.SessionDependencies
	if args.Get(0) != nil {
		deps = args.Get(0).(ports.SessionDependencies)
	}
	var hManager ports.HistoryManager
	if args.Get(1) != nil {
		hManager = args.Get(1).(ports.HistoryManager)
	}
	return deps, hManager, args.Get(2).(func(context.Context) error), args.Error(3)
}

func (m *mockSessionLifecycleManager) FinalizeSession(ctx context.Context, hManager ports.HistoryManager, deps ports.SessionDependencies, cfg *config.Config) error {
	args := m.Called(ctx, hManager, deps, cfg)
	return args.Error(0)
}

// MockSessionLifecycleManager is a mock of agent.SessionLifecycleManager.
type MockSessionLifecycleManager = mockSessionLifecycleManager
