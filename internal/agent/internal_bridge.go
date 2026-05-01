// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

// This file is the bridge between *agent and the agentinternal sibling
// package. Every method below is suffixed "ForInternalUse" to brand it
// as not-for-production at every call site. They satisfy the
// InternalAccessor interface methods consumed by agentinternal.
//
// CI guard: outside of this file and internal/agent/agentinternal/,
// no other file may reference any "ForInternalUse" symbol. See
// scripts/check_no_test_imports.sh and ADR-022.

// NewBareForInternalUse constructs an uninitialized *agent for the
// agentinternal sibling package. Tests that need to exercise narrow
// code paths (e.g. a Shutdown error path on a half-built agent) use
// agentinternal.NewBareAgent which calls this. NOT for production use.
func NewBareForInternalUse() InternalAccessor {
	return &agent{}
}

// AsChatter returns the underlying *agent typed as ports.Chatter. Used
// by agentinternal to pass the agent into production functions that
// accept the ports.Chatter interface.
func (a *agent) AsChatter() ports.Chatter {
	return a
}

// GetTracker returns the agent's domain_pricing.CostTracker.
//
// Unlike the other accessors below, GetTracker has confirmed production
// callers in infrastructure/factory/chatter.go and
// infrastructure/di/container.go. It is therefore deliberately NOT
// suffixed "ForInternalUse". Removal is tracked by issue #87. See
// ADR-022.
func (a *agent) GetTracker() domain_pricing.CostTracker {
	return a.tracker
}

// ---------------------------------------------------------------------
// Typed read-only accessors (consumed by agentinternal wrappers).
// ---------------------------------------------------------------------

func (a *agent) GetCtxManagerForInternalUse() *sessctx.Manager {
	return a.ctxManager
}

func (a *agent) GetEventsForInternalUse() events.EventBus {
	return a.events
}

func (a *agent) GetConfigWatcherForInternalUse() session.ConfigWatcher {
	return a.configWatcher
}

// GetRuntimeSnapshotForInternalUse returns a value-type snapshot of the
// agent's current runtime configuration. agentinternal converts this to
// its public RuntimeSnapshot type. This avoids leaking the unexported
// runtimeConfig type to the bridge package.
func (a *agent) GetRuntimeSnapshotForInternalUse() (snap struct {
	ProviderName     string
	Model            string
	Mode             string
	PricingOverrides map[string]domain_pricing.ModelPricing
	Limits           events.Limits
}) {
	cfg := a.config.Load()
	if cfg == nil {
		return
	}
	snap.ProviderName = cfg.ProviderName
	snap.Model = cfg.Model
	snap.Mode = cfg.Mode
	snap.PricingOverrides = cfg.PricingOverrides
	snap.Limits = cfg.Limits
	return
}

// ---------------------------------------------------------------------
// Test-only mutators (consumed by agentinternal *ForTest wrappers).
// ---------------------------------------------------------------------

func (a *agent) SetEventsForInternalUse(bus events.EventBus) {
	a.events = bus
}

func (a *agent) SetConfigWatcherForInternalUse(cw session.ConfigWatcher) {
	a.configWatcher = cw
}

func (a *agent) SetCtxManagerForInternalUse(cm *sessctx.Manager) {
	a.ctxManager = cm
}

func (a *agent) SetLoggerForInternalUse(l ports.Logger) {
	a.logger = l
}

func (a *agent) SetTrackerForInternalUse(t domain_pricing.CostTracker) {
	a.tracker = t
}

// SetRuntimeConfigForInternalUse replaces the agent's atomic runtime
// config pointer. Takes the snapshot's individual fields rather than a
// struct, to avoid having to re-export runtimeConfig (or alias it) for
// the bridge package's typed wrapper.
func (a *agent) SetRuntimeConfigForInternalUse(
	providerName, model, mode string,
	pricingOverrides map[string]domain_pricing.ModelPricing,
	limits events.Limits,
) {
	a.config.Store(&runtimeConfig{
		ProviderName:     providerName,
		Model:            model,
		Mode:             mode,
		PricingOverrides: pricingOverrides,
		Limits:           limits,
	})
}
