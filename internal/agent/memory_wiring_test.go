// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

// newMemoryTestAgent constructs a fully-initialized agent with a SpyLogger
// plus the given options, returning the Chatter interface, the white-box
// *agent, and the logger for assertion.
func newMemoryTestAgent(t *testing.T, opts ...AgentOption) (ports.Chatter, *agent, *testfixtures.SpyLogger) {
	t.Helper()

	ctx := context.Background()
	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	reg := agenttest.NewMockToolRegistry()
	sm := &mockSecurityManager{AllowAll: true}
	spy := &testfixtures.SpyLogger{}

	all := append([]AgentOption{
		WithSecurityManager(sm),
		WithProviderName("test-provider"),
		WithPricing("test-model", "test-mode", nil),
		WithLogger(spy),
	}, opts...)

	chatter, err := NewAgent(gw, bus, reg, all...)
	if err != nil {
		t.Fatalf("NewAgent failed: %v", err)
	}
	return chatter, chatter.(*agent), spy
}

// TestNewAgent_MemoryEnabledAbsentServer_WarnsAndDisables verifies the
// two-stage fallback (ADR-068 §5): an enabled-but-absent server (nil MCP
// client with a non-empty seed SERVER) yields a successful NewAgent, a
// memory_server_unavailable warn, an effectively-disabled shared config
// snapshot, and a constructed (inert, nil-client) hook. Structural wiring
// proof (injector in the pipeline, hook firing) is deferred to the T7 E2E;
// this test proves construction + fail-open posture.
func TestNewAgent_MemoryEnabledAbsentServer_WarnsAndDisables(t *testing.T) {
	seed := domain_config.MemoryConfig{
		Enabled:             true,
		Server:              "plur",
		InjectBudget:        2000,
		LearnTier:           domain_config.MemoryLearnBatch,
		MaxLearnsPerSession: 3,
	}

	_, a, spy := newMemoryTestAgent(t, WithMemoryClient(nil, seed))

	if !spy.CalledWith("Warn", "memory_server_unavailable") {
		t.Error("expected memory_server_unavailable warn for enabled-but-absent server")
	}
	// Effective disable: the shared snapshot must be non-nil and disabled.
	if cfg := a.memoryCfg.Load(); cfg == nil || cfg.Enabled {
		t.Errorf("memoryCfg snapshot = %+v; want non-nil with Enabled=false", cfg)
	}
	// The hook was constructed (once, at initComponents) with the nil client.
	if a.memoryHook == nil {
		t.Error("expected memoryHook to be constructed for enabled-but-absent server")
	}
}

// TestNewAgent_NoMemory_NoWarnNoComponents verifies the default posture:
// without WithMemoryClient the agent constructs NO memory components, emits
// no memory_server_unavailable warn, and leaves memoryHook nil — zero
// behavior change for existing users (ADR-068 §5).
func TestNewAgent_NoMemory_NoWarnNoComponents(t *testing.T) {
	_, a, spy := newMemoryTestAgent(t) // default: no memory options

	if spy.CalledWith("Warn", "memory_server_unavailable") {
		t.Error("expected NO memory_server_unavailable warn when memory is not configured")
	}
	if a.memoryHook != nil {
		t.Error("expected memoryHook nil when memory is not configured")
	}
}
