// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// This file exports typed accessor functions for unexported agent fields so
// that same-package _test.go files (both package agent and package agent_test)
// and cross-package test helpers (agentinternal) can read and mutate internal
// state without unsafe casts or reflect.
//
// All functions accept ports.Chatter and use a type assertion internally,
// making them safe for cross-package callers that hold a ports.Chatter
// reference.
//
// Per ADR-027, this file MUST NOT import "testing" and MUST NOT contain test
// logic or assertions. Cross-package test construction MUST use the
// AgentBuilder in the agentinternal sub-package instead.

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// --- Getters for same-package and cross-package test assertions ---

// EventsForTest returns the agent's internal EventBus, or nil if c is not an *agent.
func EventsForTest(c ports.Chatter) events.EventBus {
	if a, ok := c.(*agent); ok {
		return a.events
	}
	return nil
}

// CtxManagerForTest returns the agent's internal ContextManager, or nil if c is not an *agent.
func CtxManagerForTest(c ports.Chatter) *session.ContextManager {
	if a, ok := c.(*agent); ok {
		return a.ctxManager
	}
	return nil
}

// ConfigWatcherForTest returns the agent's internal ConfigWatcher, or nil if c is not an *agent.
func ConfigWatcherForTest(c ports.Chatter) session.ConfigWatcher {
	if a, ok := c.(*agent); ok {
		return a.configWatcher
	}
	return nil
}

// TrackerForTest returns the agent's internal CostTracker, or nil if c is not an *agent.
func TrackerForTest(c ports.Chatter) domain_pricing.CostTracker {
	if a, ok := c.(*agent); ok {
		return a.tracker
	}
	return nil
}

// LoggerForTest returns the agent's internal Logger, or nil if c is not an *agent.
func LoggerForTest(c ports.Chatter) ports.Logger {
	if a, ok := c.(*agent); ok {
		return a.logger
	}
	return nil
}

// RuntimeConfigForTest returns a snapshot of the agent's runtime configuration, or nil if c is not an *agent.
func RuntimeConfigForTest(c ports.Chatter) *runtimeConfig {
	if a, ok := c.(*agent); ok {
		return a.config.Load()
	}
	return nil
}

// EngineForTest returns the agent's internal Engine, or nil if c is not an *agent.
func EngineForTest(c ports.Chatter) *orchestrator.Engine {
	if a, ok := c.(*agent); ok {
		return a.engine
	}
	return nil
}

// --- Setters for test injection (used via agentinternal.AgentBuilder) ---

// SetEventsForTest replaces the agent's internal EventBus. No-op if c is not an *agent.
func SetEventsForTest(c ports.Chatter, e events.EventBus) {
	if a, ok := c.(*agent); ok {
		a.events = e
	}
}

// SetCtxManagerForTest replaces the agent's internal ContextManager. No-op if c is not an *agent.
func SetCtxManagerForTest(c ports.Chatter, cm *session.ContextManager) {
	if a, ok := c.(*agent); ok {
		a.ctxManager = cm
	}
}

// SetConfigWatcherForTest replaces the agent's internal ConfigWatcher. No-op if c is not an *agent.
func SetConfigWatcherForTest(c ports.Chatter, cw session.ConfigWatcher) {
	if a, ok := c.(*agent); ok {
		a.configWatcher = cw
	}
}

// SetTrackerForTest replaces the agent's internal CostTracker. No-op if c is not an *agent.
func SetTrackerForTest(c ports.Chatter, t domain_pricing.CostTracker) {
	if a, ok := c.(*agent); ok {
		a.tracker = t
	}
}

// SetLoggerForTest replaces the agent's internal Logger. No-op if c is not an *agent.
func SetLoggerForTest(c ports.Chatter, l ports.Logger) {
	if a, ok := c.(*agent); ok {
		a.logger = l
	}
}

// SetRuntimeConfigForTest replaces the agent's runtime configuration. No-op if c is not an *agent.
func SetRuntimeConfigForTest(c ports.Chatter, rc *runtimeConfig) {
	if a, ok := c.(*agent); ok {
		a.config.Store(rc)
	}
}
