//go:build test_helpers
// +build test_helpers

// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agentinternal exposes test helpers that legitimately need to
// import the parent internal/agent package — typed accessors for its
// internal state, and mocks of interfaces it owns whose method
// signatures reference internal/agent types.
//
// This file is guarded by the test_helpers build tag because it calls
// functions from internal/agent/internal_accessors.go (which is also
// tagged). Without this tag the package is effectively empty; any file
// that imports agentinternal must also carry the test_helpers tag.
//
// These helpers cannot live in internal/agent/agenttest because that
// package must remain a leaf with no upward dependency on
// internal/agent: any such dependency creates an import cycle for
// internal/agent's own internal-package _test.go files (those declared
// `package agent` rather than `package agent_test`).
//
// Per ADR-027, this file MUST NOT import "testing".
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

// agentInternal provides a wrapper around the agent's internal accessor.
type agentInternal struct {
	chatter ports.Chatter
	inner   agent.InternalAccessor
}

// AsAgentInternal wraps a ports.Chatter to provide access to internal components.
func AsAgentInternal(c ports.Chatter) *agentInternal {
	inner := agent.AsInternal(c)
	if inner == nil {
		return nil
	}
	return &agentInternal{chatter: c, inner: inner}
}

// --- Getters via internal_accessors.go functions ---

// GetCtxManager returns the agent's internal ContextManager.
func (a *agentInternal) GetCtxManager() *session.ContextManager {
	return agent.CtxManagerForTest(a.chatter)
}

// GetEvents returns the agent's internal EventBus.
func (a *agentInternal) GetEvents() events.EventBus {
	return agent.EventsForTest(a.chatter)
}

// GetConfigWatcher returns the agent's internal ConfigWatcher.
func (a *agentInternal) GetConfigWatcher() session.ConfigWatcher {
	return agent.ConfigWatcherForTest(a.chatter)
}

// GetRuntimeConfig returns the agent's runtime configuration snapshot.
func (a *agentInternal) GetRuntimeConfig() any {
	return agent.RuntimeConfigForTest(a.chatter)
}

// --- Setters via internal_accessors.go functions ---

// SetCtxManager replaces the agent's internal ContextManager.
func (a *agentInternal) SetCtxManager(cm *session.ContextManager) {
	agent.SetCtxManagerForTest(a.chatter, cm)
}

// SetEvents replaces the agent's internal EventBus.
func (a *agentInternal) SetEvents(bus events.EventBus) {
	agent.SetEventsForTest(a.chatter, bus)
}

// SetConfigWatcher replaces the agent's internal ConfigWatcher.
func (a *agentInternal) SetConfigWatcher(cw session.ConfigWatcher) {
	agent.SetConfigWatcherForTest(a.chatter, cw)
}

// SetLogger replaces the agent's internal Logger.
func (a *agentInternal) SetLogger(l ports.Logger) {
	agent.SetLoggerForTest(a.chatter, l)
}

// SetRuntimeConfig replaces the agent's runtime configuration.
func (a *agentInternal) SetRuntimeConfig(cfg any) {
	if rc, ok := cfg.(*agent.RuntimeConfigInternal); ok {
		agent.SetRuntimeConfigForTest(a.chatter, rc)
	}
}

// SetTracker replaces the agent's internal CostTracker.
func (a *agentInternal) SetTracker(t domain_pricing.CostTracker) {
	agent.SetTrackerForTest(a.chatter, t)
}

// --- Pass-through methods for remaining InternalAccessor members ---

// ApplyConfig delegates to the internal accessor.
func (a *agentInternal) ApplyConfig(ctx context.Context) error {
	return a.inner.ApplyConfig(ctx)
}

// GetTracker delegates to the internal accessor.
func (a *agentInternal) GetTracker() domain_pricing.CostTracker {
	return a.inner.GetTracker()
}

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
