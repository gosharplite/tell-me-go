// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/session"
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
		cw := session.NewNoOpConfigWatcher(1000, 10, 5)
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
