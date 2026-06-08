// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"log/slog"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
)

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
		tracker := &agenttest.MockCostTracker{}
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
		tracker := &agenttest.MockCostTracker{}

		a.SetTrackerForInternalUse(tracker)
		if a.tracker != tracker {
			t.Errorf("after SetTrackerForInternalUse, a.tracker = %v; want %v", a.tracker, tracker)
		}
	})
}

// TestGetRuntimeSnapshotForInternalUse_FieldMapping verifies that every field
// from the agent's internal runtimeConfig is correctly copied into the
// anonymous struct returned by GetRuntimeSnapshotForInternalUse.
//
// This guards against the scenario where a field already exists in both
// runtimeConfig and the snapshot struct, but is accidentally removed or
// omitted from the copy logic at internal_bridge.go:76-81. For type-level
// drift detection (new fields added to runtimeConfig but not to the
// snapshot struct), see TestGetRuntimeSnapshotForInternalUse_TypeParity.
// See issue #273.
func TestGetRuntimeSnapshotForInternalUse_FieldMapping(t *testing.T) {
	// Use a bare agent — no full initialization needed.
	a := &agent{}

	// Seed the atomic config via the write-path bridge.
	a.SetRuntimeConfigForInternalUse(
		"openai", // providerName
		"gpt-4o", // model
		"chat",   // mode
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

	// Assert each field independently via subtests so failures pinpoint the exact field.
	t.Run("ProviderName", func(t *testing.T) {
		assertProviderName(t, snap.ProviderName)
	})
	t.Run("Model", func(t *testing.T) {
		assertModel(t, snap.Model)
	})
	t.Run("Mode", func(t *testing.T) {
		assertMode(t, snap.Mode)
	})
	t.Run("PricingOverrides", func(t *testing.T) {
		assertPricingOverrides(t, snap.PricingOverrides)
	})
	t.Run("Limits", func(t *testing.T) {
		assertLimits(t, snap.Limits)
	})
}

func assertProviderName(t *testing.T, got string) {
	t.Helper()
	if got != "openai" {
		t.Errorf("ProviderName = %q; want %q", got, "openai")
	}
}

func assertModel(t *testing.T, got string) {
	t.Helper()
	if got != "gpt-4o" {
		t.Errorf("Model = %q; want %q", got, "gpt-4o")
	}
}

func assertMode(t *testing.T, got string) {
	t.Helper()
	if got != "chat" {
		t.Errorf("Mode = %q; want %q", got, "chat")
	}
}

func assertPricingOverrides(t *testing.T, got map[string]domain_pricing.ModelPricing) {
	t.Helper()
	if len(got) != 1 {
		t.Errorf("PricingOverrides len = %d; want 1", len(got))
	} else if p, ok := got["gpt-4o"]; !ok {
		t.Errorf("PricingOverrides missing key %q", "gpt-4o")
	} else if p.Hit != 0.01 || p.Miss != 0.02 {
		t.Errorf("PricingOverrides[gpt-4o] = %+v; want {Hit:0.01 Miss:0.02}", p)
	}
}

func assertLimits(t *testing.T, got events.Limits) {
	t.Helper()
	if got.MaxHistoryTokens != 4000 {
		t.Errorf("Limits.MaxHistoryTokens = %d; want 4000", got.MaxHistoryTokens)
	}
	if got.MaxToolTurns != 15 {
		t.Errorf("Limits.MaxToolTurns = %d; want 15", got.MaxToolTurns)
	}
	if got.MaxHistoryTurns != 8 {
		t.Errorf("Limits.MaxHistoryTurns = %d; want 8", got.MaxHistoryTurns)
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

// TestGetRuntimeSnapshotForInternalUse_TypeParity uses reflection to enforce
// structural parity between the unexported runtimeConfig struct and the
// anonymous struct returned by GetRuntimeSnapshotForInternalUse.
//
// If a developer adds a field to runtimeConfig but forgets to add it to the
// snapshot struct (or changes its type), this test fails immediately with a
// precise message naming the mismatched field. This is a compile-time-adjacent
// architectural guard that the FieldMapping test alone cannot provide.
//
// See issue #273.
func TestGetRuntimeSnapshotForInternalUse_TypeParity(t *testing.T) {
	cfgType := reflect.TypeOf(runtimeConfig{})
	snapType := reflect.TypeOf((&agent{}).GetRuntimeSnapshotForInternalUse())

	// 1. Field count must match.
	if cfgType.NumField() != snapType.NumField() {
		t.Errorf("field count mismatch: runtimeConfig has %d fields, snapshot has %d",
			cfgType.NumField(), snapType.NumField())
	}

	// 2. Every runtimeConfig field must exist in the snapshot with the same type.
	for i := 0; i < cfgType.NumField(); i++ {
		cfgField := cfgType.Field(i)
		snapField, ok := snapType.FieldByName(cfgField.Name)
		if !ok {
			t.Errorf("snapshot is missing field %q (present in runtimeConfig)", cfgField.Name)
			continue
		}
		if cfgField.Type != snapField.Type {
			t.Errorf("field %q type mismatch: runtimeConfig=%v, snapshot=%v",
				cfgField.Name, cfgField.Type, snapField.Type)
		}
	}
}
