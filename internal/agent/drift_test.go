// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// TestDiffConfig_AllInSync verifies that a freshly-initialized agent
// reports InSync after ApplyConfig has been called — the canonical
// config, engine, and context manager all match.
func TestDiffConfig_AllInSync(t *testing.T) {
	chatter, accessor := newTestAgent(t)
	_ = chatter // keep alive

	if err := accessor.ApplyConfig(context.Background()); err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	report := accessor.DiffConfig()

	if !report.InSync {
		t.Error("expected InSync == true after ApplyConfig")
	}
	if report.EngineDrift != nil {
		t.Errorf("expected EngineDrift to be nil, got %+v", report.EngineDrift)
	}
	if report.CtxManagerDrift != nil {
		t.Errorf("expected CtxManagerDrift to be nil, got %+v", report.CtxManagerDrift)
	}
}

// TestDiffConfig_EngineDrift verifies that EngineDrift is detected
// when the canonical config is mutated without calling ApplyConfig,
// leaving the engine on its original configuration.
func TestDiffConfig_EngineDrift(t *testing.T) {
	_, accessor := newTestAgent(t)

	// Mutate the canonical config to values that differ from what
	// the engine received during NewAgent. Do NOT call ApplyConfig —
	// this leaves the engine on the original config while a.config
	// reflects the new values.
	accessor.SetRuntimeConfigForInternalUse(
		"different-provider",
		"different-model",
		"different-mode",
		nil, // pricing overrides
		events.Limits{
			MaxHistoryTokens: 120000,
			MaxToolTurns:     200,
			MaxHistoryTurns:  0,
		},
	)

	report := accessor.DiffConfig()

	if report.InSync {
		t.Error("expected InSync == false when engine has drifted")
	}
	if report.EngineDrift == nil {
		t.Fatal("expected EngineDrift to be non-nil")
	}
	if report.EngineDrift.ExpectedProvider != "different-provider" {
		t.Errorf("ExpectedProvider = %q; want %q", report.EngineDrift.ExpectedProvider, "different-provider")
	}
	if report.EngineDrift.ExpectedModel != "different-model" {
		t.Errorf("ExpectedModel = %q; want %q", report.EngineDrift.ExpectedModel, "different-model")
	}
	// ActualProvider/Model/Mode should be the original values from NewAgent
	if report.EngineDrift.ActualProvider != "test-provider" {
		t.Errorf("ActualProvider = %q; want %q", report.EngineDrift.ActualProvider, "test-provider")
	}
	if report.EngineDrift.ActualModel != "test-model" {
		t.Errorf("ActualModel = %q; want %q", report.EngineDrift.ActualModel, "test-model")
	}

	// CtxManager should still be in sync (we didn't change limits
	// relative to what was already stored; the mutated limits
	// match the defaults from NewAgent which ApplyConfig propagated)
	if report.CtxManagerDrift != nil {
		t.Errorf("expected CtxManagerDrift to be nil, got %+v", report.CtxManagerDrift)
	}
}

// TestDiffConfig_CtxManagerDrift verifies that CtxManagerDrift is
// detected when limits are pushed into the context manager via
// ApplyConfig with a stubConfigWatcher, then the canonical config is
// mutated to different limits without re-applying.
func TestDiffConfig_CtxManagerDrift(t *testing.T) {
	_, accessor := newTestAgent(t)

	// Step 1: Push limits into ctxManager via ApplyConfig.
	accessor.SetConfigWatcherForInternalUse(&stubConfigWatcher{
		tokens:       240000,
		toolTurns:    400,
		historyTurns: 100,
	})
	if err := accessor.ApplyConfig(context.Background()); err != nil {
		t.Fatalf("ApplyConfig failed: %v", err)
	}

	// Step 2: Mutate the canonical config to different limits.
	// Do NOT call ApplyConfig — ctxManager keeps the old limits.
	accessor.SetRuntimeConfigForInternalUse(
		"test-provider",
		"test-model",
		"test-mode",
		nil,
		events.Limits{
			MaxHistoryTokens: 120000,
			MaxToolTurns:     200,
			MaxHistoryTurns:  0,
		},
	)

	report := accessor.DiffConfig()

	if report.InSync {
		t.Error("expected InSync == false when ctx manager has drifted")
	}
	if report.CtxManagerDrift == nil {
		t.Fatal("expected CtxManagerDrift to be non-nil")
	}
	if report.CtxManagerDrift.ExpectedMaxHistoryTokens != 120000 {
		t.Errorf("ExpectedMaxHistoryTokens = %d; want 120000", report.CtxManagerDrift.ExpectedMaxHistoryTokens)
	}
	if report.CtxManagerDrift.ExpectedMaxToolTurns != 200 {
		t.Errorf("ExpectedMaxToolTurns = %d; want 200", report.CtxManagerDrift.ExpectedMaxToolTurns)
	}
	if report.CtxManagerDrift.ActualMaxHistoryTokens != 240000 {
		t.Errorf("ActualMaxHistoryTokens = %d; want 240000", report.CtxManagerDrift.ActualMaxHistoryTokens)
	}
	if report.CtxManagerDrift.ActualMaxToolTurns != 400 {
		t.Errorf("ActualMaxToolTurns = %d; want 400", report.CtxManagerDrift.ActualMaxToolTurns)
	}

	// Engine should still be in sync
	if report.EngineDrift != nil {
		t.Errorf("expected EngineDrift to be nil, got %+v", report.EngineDrift)
	}
}

// TestDiffConfig_BothDrift verifies that both EngineDrift and
// CtxManagerDrift are detected simultaneously when the canonical
// config is mutated in both dimensions (engine fields AND limits)
// without calling ApplyConfig.
func TestDiffConfig_BothDrift(t *testing.T) {
	_, accessor := newTestAgent(t)

	// Mutate both engine fields AND limits without ApplyConfig.
	accessor.SetRuntimeConfigForInternalUse(
		"different-provider",
		"different-model",
		"different-mode",
		nil, // pricing overrides
		events.Limits{
			MaxHistoryTokens: 999000,
			MaxToolTurns:     999,
			MaxHistoryTurns:  99,
		},
	)

	report := accessor.DiffConfig()

	if report.InSync {
		t.Error("expected InSync == false when both engine and ctx manager have drifted")
	}
	if report.EngineDrift == nil {
		t.Fatal("expected EngineDrift to be non-nil")
	}
	if report.CtxManagerDrift == nil {
		t.Fatal("expected CtxManagerDrift to be non-nil")
	}

	// Engine drift: expected from canonical, actual from engine initial.
	if report.EngineDrift.ExpectedProvider != "different-provider" {
		t.Errorf("ExpectedProvider = %q; want %q", report.EngineDrift.ExpectedProvider, "different-provider")
	}
	if report.EngineDrift.ActualProvider != "test-provider" {
		t.Errorf("ActualProvider = %q; want %q", report.EngineDrift.ActualProvider, "test-provider")
	}

	// CtxManager drift: expected from canonical, actual from defaults.
	if report.CtxManagerDrift.ExpectedMaxHistoryTokens != 999000 {
		t.Errorf("ExpectedMaxHistoryTokens = %d; want 999000", report.CtxManagerDrift.ExpectedMaxHistoryTokens)
	}
	if report.CtxManagerDrift.ExpectedMaxToolTurns != 999 {
		t.Errorf("ExpectedMaxToolTurns = %d; want 999", report.CtxManagerDrift.ExpectedMaxToolTurns)
	}
}

// TestDiffConfig_NilEngineAndCtxManager verifies that a bare agent
// (nil engine, nil ctxManager) with a seeded config reports InSync
// — nil components have no drift by definition.
func TestDiffConfig_NilEngineAndCtxManager(t *testing.T) {
	accessor := NewBareForInternalUse()

	// Seed config so that DiffConfig has a canonical config to compare
	// against. Even with a canonical config, nil engine and nil
	// ctxManager mean there is nothing to drift.
	accessor.SetRuntimeConfigForInternalUse(
		"test-provider",
		"test-model",
		"test-mode",
		nil,
		events.Limits{
			MaxHistoryTokens: 120000,
			MaxToolTurns:     200,
			MaxHistoryTurns:  0,
		},
	)

	report := accessor.DiffConfig()

	if !report.InSync {
		t.Error("expected InSync == true when engine and ctxManager are nil")
	}
	if report.EngineDrift != nil {
		t.Errorf("expected EngineDrift to be nil with nil engine, got %+v", report.EngineDrift)
	}
	if report.CtxManagerDrift != nil {
		t.Errorf("expected CtxManagerDrift to be nil with nil ctxManager, got %+v", report.CtxManagerDrift)
	}
}

// TestDiffConfig_NilConfig verifies that a bare agent without any
// seeded config reports InSync — a nil canonical config means there
// is nothing to compare against.
func TestDiffConfig_NilConfig(t *testing.T) {
	accessor := NewBareForInternalUse()

	// Do NOT seed any config. The atomic pointer is nil.

	report := accessor.DiffConfig()

	if !report.InSync {
		t.Error("expected InSync == true when canonical config is nil")
	}
}
