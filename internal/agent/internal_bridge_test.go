// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"testing"

	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/stretchr/testify/mock"
)

// mockCostTracker is a minimal mock of domain_pricing.CostTracker used
// by bridge tests that only need a concrete value to inject/extract
// — no method expectations are set.
type mockCostTracker struct {
	mock.Mock
}

func (m *mockCostTracker) GetTotalCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetDailyCost(ctx context.Context) float64 {
	return m.Called(ctx).Get(0).(float64)
}

func (m *mockCostTracker) GetStats(ctx context.Context) (domain_pricing.UsageStats, float64) {
	args := m.Called(ctx)
	return args.Get(0).(domain_pricing.UsageStats), args.Get(1).(float64)
}

func (m *mockCostTracker) Accumulate(mt llm.Metrics) {
	m.Called(mt)
}

func (m *mockCostTracker) AccumulateAndReturn(mt llm.Metrics) float64 {
	return m.Called(mt).Get(0).(float64)
}

func (m *mockCostTracker) Warmup() {
	m.Called()
}

var _ domain_pricing.CostTracker = (*mockCostTracker)(nil)

// TestInternalBridge_GettersAndSetters verifies that the five orphan
// bridge methods on *agent — GetTrackerForInternalUse,
// GetEventsForInternalUse, GetConfigWatcherForInternalUse,
// SetLoggerForInternalUse, and SetTrackerForInternalUse — correctly
// read from or write to the corresponding unexported fields.
//
// These five methods have no transitive coverage path today: they are
// not exercised by agentinternal tests (which use mocks), nor by
// applyconfig_failpath_test.go. A signature mismatch or a renamed
// field would go undetected without this test.
func TestInternalBridge_GettersAndSetters(t *testing.T) {
	t.Run("Getter/GetTrackerForInternalUse", func(t *testing.T) {
		a := &agent{}
		tracker := &mockCostTracker{}
		a.tracker = tracker

		got := a.GetTrackerForInternalUse()
		if got != tracker {
			t.Errorf("GetTrackerForInternalUse() = %v; want %v", got, tracker)
		}
	})

	t.Run("Getter/GetEventsForInternalUse", func(t *testing.T) {
		a := &agent{}
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		t.Cleanup(func() { _ = bus.Shutdown(context.Background()) })
		a.events = bus

		got := a.GetEventsForInternalUse()
		if got != bus {
			t.Errorf("GetEventsForInternalUse() = %v; want %v", got, bus)
		}
	})

	t.Run("Getter/GetConfigWatcherForInternalUse", func(t *testing.T) {
		a := &agent{}
		cw := domain_config.NewNoOpConfigWatcher(1000, 10, 5)
		a.configWatcher = cw

		got := a.GetConfigWatcherForInternalUse()
		if got != cw {
			t.Errorf("GetConfigWatcherForInternalUse() = %v; want %v", got, cw)
		}
	})

	t.Run("Setter/SetLoggerForInternalUse", func(t *testing.T) {
		a := &agent{}
		logger := slog.Default()

		a.SetLoggerForInternalUse(logger)
		if a.logger != logger {
			t.Errorf("after SetLoggerForInternalUse, a.logger = %v; want %v", a.logger, logger)
		}
	})

	t.Run("Setter/SetTrackerForInternalUse", func(t *testing.T) {
		a := &agent{}
		tracker := &mockCostTracker{}

		a.SetTrackerForInternalUse(tracker)
		if a.tracker != tracker {
			t.Errorf("after SetTrackerForInternalUse, a.tracker = %v; want %v", a.tracker, tracker)
		}
	})
}

// TestGetRuntimeSnapshotForInternalUse_FullCopy verifies that every field
// from the agent's internal runtimeConfig is correctly copied into the
// anonymous struct returned by GetRuntimeSnapshotForInternalUse.
//
// This guards against field-drift: if a new field is added to runtimeConfig
// but forgotten in the snapshot mapping, consumers silently receive zero
// values. See issue #273.
func TestGetRuntimeSnapshotForInternalUse_FullCopy(t *testing.T) {
	// Use a bare agent — no full initialization needed.
	a := &agent{}

	// Seed the atomic config via the write-path bridge.
	a.SetRuntimeConfigForInternalUse(
		"openai",     // providerName
		"gpt-4o",     // model
		"chat",       // mode
		map[string]domain_pricing.ModelPricing{
			"gpt-4o": {Hit: 0.01, Miss: 0.02},
		},
		events.Limits{
			MaxHistoryTokens: 4000,
			MaxToolTurns:     15,
			MaxHistoryTurns:  8,
		},
	)

	// Read back via the snapshot method under test.
	snap := a.GetRuntimeSnapshotForInternalUse()

	// Assert each field independently so failures pinpoint the exact field.
	if snap.ProviderName != "openai" {
		t.Errorf("ProviderName = %q; want %q", snap.ProviderName, "openai")
	}
	if snap.Model != "gpt-4o" {
		t.Errorf("Model = %q; want %q", snap.Model, "gpt-4o")
	}
	if snap.Mode != "chat" {
		t.Errorf("Mode = %q; want %q", snap.Mode, "chat")
	}
	if len(snap.PricingOverrides) != 1 {
		t.Errorf("PricingOverrides len = %d; want 1", len(snap.PricingOverrides))
	} else if p, ok := snap.PricingOverrides["gpt-4o"]; !ok {
		t.Errorf("PricingOverrides missing key %q", "gpt-4o")
	} else if p.Hit != 0.01 || p.Miss != 0.02 {
		t.Errorf("PricingOverrides[gpt-4o] = %+v; want {Hit:0.01 Miss:0.02}", p)
	}
	if snap.Limits.MaxHistoryTokens != 4000 {
		t.Errorf("Limits.MaxHistoryTokens = %d; want 4000", snap.Limits.MaxHistoryTokens)
	}
	if snap.Limits.MaxToolTurns != 15 {
		t.Errorf("Limits.MaxToolTurns = %d; want 15", snap.Limits.MaxToolTurns)
	}
	if snap.Limits.MaxHistoryTurns != 8 {
		t.Errorf("Limits.MaxHistoryTurns = %d; want 8", snap.Limits.MaxHistoryTurns)
	}
}

// TestGetRuntimeSnapshotForInternalUse_NilConfig verifies the early-return
// path when the agent's atomic config pointer has never been stored.
//
// A bare agent (constructed via &agent{}) has a nil atomic.Pointer; calling
// Load() on it returns nil. The method must return a zero-valued struct
// without panicking.
func TestGetRuntimeSnapshotForInternalUse_NilConfig(t *testing.T) {
	// Bare agent: no config.Store() call, so a.config.Load() returns nil.
	a := &agent{}

	snap := a.GetRuntimeSnapshotForInternalUse()

	// All fields must be zero-valued.
	if snap.ProviderName != "" {
		t.Errorf("ProviderName = %q; want empty string", snap.ProviderName)
	}
	if snap.Model != "" {
		t.Errorf("Model = %q; want empty string", snap.Model)
	}
	if snap.Mode != "" {
		t.Errorf("Mode = %q; want empty string", snap.Mode)
	}
	if snap.PricingOverrides != nil {
		t.Errorf("PricingOverrides = %v; want nil map", snap.PricingOverrides)
	}
	if snap.Limits != (events.Limits{}) {
		t.Errorf("Limits = %+v; want zero value", snap.Limits)
	}
}
