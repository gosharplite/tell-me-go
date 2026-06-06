// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestratortest

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

// fakeT is a lightweight fake testing.T for asserting error paths.
type fakeT struct {
	errs []string
}

func (f *fakeT) Errorf(format string, args ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

func (f *fakeT) Helper() {}

// cleanupRecordingT extends fakeT with cleanup tracking for verifying
// that SetupTurnEngineTest registers a cleanup function.
type cleanupRecordingT struct {
	fakeT
	cleanupFuncs []func()
}

func (c *cleanupRecordingT) Cleanup(f func()) {
	c.cleanupFuncs = append(c.cleanupFuncs, f)
}

// ────────────────────────────── MockHook ──────────────────────────────

func TestMockHook(t *testing.T) {
	t.Parallel()

	t.Run("BeforeTurn_increments", func(t *testing.T) {
		t.Parallel()
		h := &MockHook{}
		h.BeforeTurn(nil)
		h.BeforeTurn(nil)
		if h.BeforeCalled != 2 {
			t.Errorf("BeforeCalled = %d; want 2", h.BeforeCalled)
		}
	})

	t.Run("AfterTurn_increments", func(t *testing.T) {
		t.Parallel()
		h := &MockHook{}
		h.AfterTurn(nil, nil)
		if h.AfterCalled != 1 {
			t.Errorf("AfterCalled = %d; want 1", h.AfterCalled)
		}
	})

	t.Run("OnPhaseTransition_increments", func(t *testing.T) {
		t.Parallel()
		h := &MockHook{}
		h.OnPhaseTransition(orchestrator.PhaseGuard, orchestrator.PhaseInference, nil)
		h.OnPhaseTransition(orchestrator.PhaseInference, orchestrator.PhaseExecuting, nil)
		h.OnPhaseTransition(orchestrator.PhaseExecuting, orchestrator.PhasePersisting, nil)
		if h.TransCalled != 3 {
			t.Errorf("TransCalled = %d; want 3", h.TransCalled)
		}
	})
}

// ──────────────────────── TestMockRetryPolicy_RetryWithDuration ─────────

func TestMockRetryPolicy_RetryWithDuration(t *testing.T) {
	t.Parallel()
	p := &MockRetryPolicy{
		Retry:              true,
		ShouldRetryResults: []time.Duration{5 * time.Second},
	}
	c := &clock.RealClock{}
	d, ok := p.ShouldRetry(c, fmt.Errorf("some error"), 0, false)
	if d != 5*time.Second {
		t.Errorf("duration = %v; want 5s", d)
	}
	if !ok {
		t.Error("ShouldRetry returned false; want true")
	}
	if !p.ShouldRetryCalled {
		t.Error("ShouldRetryCalled = false; want true")
	}
}

// ──────────────────────── TestMockRetryPolicy_NoRetry ──────────────────

func TestMockRetryPolicy_NoRetry(t *testing.T) {
	t.Parallel()
	p := &MockRetryPolicy{Retry: false}
	c := &clock.RealClock{}
	d, ok := p.ShouldRetry(c, fmt.Errorf("some error"), 0, false)
	if d != 0 {
		t.Errorf("duration = %v; want 0", d)
	}
	if ok {
		t.Error("ShouldRetry returned true; want false")
	}
}

// ──────────────────────── TestMockRetryPolicy_ExhaustedResults ─────────

func TestMockRetryPolicy_ExhaustedResults(t *testing.T) {
	t.Parallel()
	p := &MockRetryPolicy{
		Retry:              true,
		ShouldRetryResults: []time.Duration{1 * time.Second},
	}
	c := &clock.RealClock{}

	// First call: attempt 0, within results slice
	d, ok := p.ShouldRetry(c, fmt.Errorf("err"), 0, false)
	if d != 1*time.Second {
		t.Errorf("first duration = %v; want 1s", d)
	}
	if !ok {
		t.Error("first ShouldRetry returned false; want true")
	}

	// Second call: attempt 1, exhausted results → returns (0, true)
	d, ok = p.ShouldRetry(c, fmt.Errorf("err"), 1, false)
	if d != 0 {
		t.Errorf("exhausted duration = %v; want 0", d)
	}
	if !ok {
		t.Error("exhausted ShouldRetry returned false; want true")
	}
}

// ──────────────────────── TestMockRetryPolicy_AttemptIndexes ───────────

func TestMockRetryPolicy_AttemptIndexes(t *testing.T) {
	t.Parallel()
	p := &MockRetryPolicy{
		Retry: true,
		ShouldRetryResults: []time.Duration{
			1 * time.Second,
			2 * time.Second,
			3 * time.Second,
		},
	}
	c := &clock.RealClock{}

	expected := []time.Duration{1 * time.Second, 2 * time.Second, 3 * time.Second}
	for i, exp := range expected {
		d, ok := p.ShouldRetry(c, fmt.Errorf("err"), i, false)
		if d != exp {
			t.Errorf("attempt %d: duration = %v; want %v", i, d, exp)
		}
		if !ok {
			t.Errorf("attempt %d: ShouldRetry returned false; want true", i)
		}
	}
}

// ──────────────────────── CreateProcessorForPhase ─────────────────────

func TestCreateProcessorForPhase(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		phase orchestrator.TurnPhase
		isNil bool
	}{
		{"PhaseGuard", orchestrator.PhaseGuard, false},
		{"PhaseRefining", orchestrator.PhaseRefining, false},
		{"PhaseInference", orchestrator.PhaseInference, false},
		{"PhaseExecuting", orchestrator.PhaseExecuting, false},
		{"PhasePersisting", orchestrator.PhasePersisting, false},
		{"PhaseRecovering", orchestrator.PhaseRecovering, false},
		{"unknown_returns_nil", orchestrator.TurnPhase("UnknownPhase"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := CreateProcessorForPhase(tt.phase)
			if tt.isNil && p != nil {
				t.Error("expected nil")
			}
			if !tt.isNil && p == nil {
				t.Error("expected non-nil")
			}
		})
	}
}

// ──────────────────────────── costCapturer ────────────────────────────
// costCapturer tests share a bus per subtest; each subtest creates its own
// bus to avoid ordering dependencies, so t.Parallel() is safe.

func TestNewCostCapturer(t *testing.T) {
	t.Parallel()

	t.Run("captures_turn_costs", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		defer func() { _ = bus.Shutdown(context.Background()) }()

		cc := NewCostCapturer(bus)

		err := bus.Publish(context.Background(), events.UsageMetricsEvent{
			Metrics: &llm.Metrics{Cost: 1.5},
		})
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}

		ft := &fakeT{}
		cc.AssertTurnCosts(ft, []float64{1.5})
		if len(ft.errs) > 0 {
			t.Errorf("unexpected errors: %v", ft.errs)
		}
	})

	t.Run("captures_task_cost", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		defer func() { _ = bus.Shutdown(context.Background()) }()

		cc := NewCostCapturer(bus)

		err := bus.Publish(context.Background(), events.TurnStatusEvent{
			Status: events.TurnStatus{TaskCost: 2.5},
		})
		if err != nil {
			t.Fatalf("Publish failed: %v", err)
		}

		ft := &fakeT{}
		cc.AssertTaskCost(ft, 2.5)
		if len(ft.errs) > 0 {
			t.Errorf("unexpected errors: %v", ft.errs)
		}
	})

	t.Run("reset_clears_state", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		defer func() { _ = bus.Shutdown(context.Background()) }()

		cc := NewCostCapturer(bus)

		// Seed some data
		_ = bus.Publish(context.Background(), events.UsageMetricsEvent{
			Metrics: &llm.Metrics{Cost: 3.0},
		})
		_ = bus.Publish(context.Background(), events.TurnStatusEvent{
			Status: events.TurnStatus{TaskCost: 4.0},
		})

		cc.Reset()

		cc.Mu.Lock()
		turnCostsNil := cc.TurnCosts == nil
		lastTaskZero := cc.LastTaskCost == 0
		cc.Mu.Unlock()

		if !turnCostsNil {
			t.Error("TurnCosts should be nil after Reset")
		}
		if !lastTaskZero {
			t.Errorf("LastTaskCost = %f; want 0", cc.LastTaskCost)
		}
	})

	t.Run("assert_task_cost_mismatch", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		defer func() { _ = bus.Shutdown(context.Background()) }()

		cc := NewCostCapturer(bus)

		// Seed with 1.0
		_ = bus.Publish(context.Background(), events.TurnStatusEvent{
			Status: events.TurnStatus{TaskCost: 1.0},
		})

		// Assert 2.0 — should record an error
		ft := &fakeT{}
		cc.AssertTaskCost(ft, 2.0)
		if len(ft.errs) == 0 {
			t.Error("expected an error but got none")
		}
	})

	t.Run("assert_turn_costs_empty_vs_nonempty", func(t *testing.T) {
		t.Parallel()
		bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		defer func() { _ = bus.Shutdown(context.Background()) }()

		cc := NewCostCapturer(bus)

		ft := &fakeT{}
		// No events published → TurnCosts is empty, expected non-empty
		cc.AssertTurnCosts(ft, []float64{1.0})
		if len(ft.errs) == 0 {
			t.Error("expected length mismatch error but got none")
		}
	})
}

// ──────────────────────── TestNewTestContextManager ──────────────────

func TestNewTestContextManager(t *testing.T) {
	t.Parallel()

	strategy := sessctx.NewStrategy(sessctx.NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	hManager := &agenttest.MockHistoryManager{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	defer func() { _ = bus.Shutdown(context.Background()) }()

	cm := NewTestContextManager(strategy, hManager, bus)

	t.Run("creates_non_nil_manager", func(t *testing.T) {
		if cm == nil {
			t.Fatal("expected non-nil manager")
		}
		if cm.Pipeline == nil {
			t.Error("expected non-nil Pipeline")
		}
	})

	t.Run("strategy_is_set", func(t *testing.T) {
		if cm.Strategy != strategy {
			t.Errorf("Strategy does not match input")
		}
	})

	t.Run("bus_is_set", func(t *testing.T) {
		if cm.Events != bus {
			t.Errorf("Events field does not match input bus")
		}
	})
}

// ─────────────────────── TestSetupTurnEngineTest ──────────────────────

func TestSetupTurnEngineTest(t *testing.T) {
	t.Parallel()

	rt := &cleanupRecordingT{}
	env := SetupTurnEngineTest(rt)

	t.Run("returns_populated_env", func(t *testing.T) {
		if env.Gw == nil {
			t.Error("Gw is nil")
		}
		if env.Reg == nil {
			t.Error("Reg is nil")
		}
		if env.Bus == nil {
			t.Error("Bus is nil")
		}
		if env.Cm == nil {
			t.Error("Cm is nil")
		}
		if env.HManager == nil {
			t.Error("HManager is nil")
		}
	})

	t.Run("seeds_default_prompt", func(t *testing.T) {
		if n := env.HManager.GetTotalEntries(); n != 1 {
			t.Errorf("GetTotalEntries() = %d; want 1", n)
		}
	})

	t.Run("cleanup_registered", func(t *testing.T) {
		if len(rt.cleanupFuncs) == 0 {
			t.Error("expected at least one cleanup to be registered")
		}
	})
}

// ──────────────────────── TestSetupTransitionTurn ────────────────────

func TestSetupTransitionTurn(t *testing.T) {
	t.Parallel()

	t.Run("no_tools_inference_phase", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(false, orchestrator.PhaseInference, nil)
		if turn.State.HasToolCalls {
			t.Error("HasToolCalls = true; want false")
		}
		if turn.Gateway == nil {
			t.Error("Gateway is nil")
		}
		if turn.Executor == nil {
			t.Error("Executor is nil")
		}
		if turn.Registry == nil {
			t.Error("Registry is nil")
		}
		if turn.TokenCounter == nil {
			t.Error("TokenCounter is nil")
		}
		if turn.Clock == nil {
			t.Error("Clock is nil")
		}
	})

	t.Run("has_tools_inference_phase", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(true, orchestrator.PhaseInference, nil)
		if !turn.State.HasToolCalls {
			t.Error("HasToolCalls = false; want true")
		}
	})

	t.Run("with_exec_error", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(true, orchestrator.PhaseInference, errors.New("boom"))
		_, err := turn.Executor.Execute(context.Background(), &llm.Content{}, 0, 0)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if err.Error() != "boom" {
			t.Errorf("error = %q; want %q", err.Error(), "boom")
		}
	})

	t.Run("guard_phase_has_pipeline", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(false, orchestrator.PhaseGuard, nil)
		if turn.CtxManager.Pipeline == nil {
			t.Error("Pipeline is nil for guard phase; want non-nil")
		}
	})

	t.Run("refining_phase_has_pipeline", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(false, orchestrator.PhaseRefining, nil)
		if turn.CtxManager.Pipeline == nil {
			t.Error("Pipeline is nil for refining phase; want non-nil")
		}
	})

	t.Run("executing_phase_no_pipeline", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(false, orchestrator.PhaseExecuting, nil)
		if turn.CtxManager.Pipeline != nil {
			t.Error("Pipeline is non-nil for executing phase; want nil")
		}
	})

	t.Run("turn_state_metadata", func(t *testing.T) {
		t.Parallel()
		turn := SetupTransitionTurn(false, orchestrator.PhaseInference, nil)
		meta := turn.State.Metadata
		if meta == nil {
			t.Fatal("Metadata is nil")
		}
		if len(meta.History) != 1 {
			t.Fatalf("Metadata.History length = %d; want 1", len(meta.History))
		}
		entry := meta.History[0]
		if entry.Role != "user" {
			t.Errorf("role = %q; want %q", entry.Role, "user")
		}
		if len(entry.Parts) == 0 || entry.Parts[0].Text != "test" {
			t.Error("text does not match expected 'test'")
		}
	})
}
