// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package agentinternal exposes test helpers that legitimately need to
// import the parent internal/agent package — typed accessors for its
// internal state, and mocks of interfaces it owns whose method
// signatures reference internal/agent types.
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
package agentinternal

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

// agentInternal provides a wrapper around the agent's internal accessor.
type agentInternal struct {
	agent.InternalAccessor
}

// AsAgentInternal wraps a ports.Chatter to provide access to internal components.
func AsAgentInternal(c ports.Chatter) *agentInternal {
	inner := agent.AsInternal(c)
	if inner == nil {
		return nil
	}
	return &agentInternal{inner}
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
