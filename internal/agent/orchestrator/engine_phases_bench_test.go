// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// newBenchTurn creates a minimal Turn for phase benchmarks.
// It uses a passThroughEventBus and a nil Factory so that Prepare
// returns raw history directly with no pipeline overhead.
func newBenchTurn() *Turn {
	counter := &agenttest.MockTokenCounter{Tokens: 500}
	hMock := &agenttest.MockHistoryManager{
		Contents: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}},
	}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, &passThroughEventBus{}, nil)
	if err := cm.Reconfigure(events.Limits{
		MaxToolTurns:     100,
		MaxHistoryTokens: 1000000,
		MaxHistoryTurns:  100,
		ContextWindow:    1000000,
	}); err != nil {
		panic(err)
	}
	return &Turn{
		Index:        1,
		CtxManager:   cm,
		Events:       &passThroughEventBus{},
		TokenCounter: counter,
		Clock:        &agenttest.MockClock{},
		State:        &TurnState{},
		Logger:       &ports.NoOpLogger{},
	}
}

// newBenchTurnWithRealBus is like newBenchTurn but uses a real
// TestEventBus for both the CtxManager and Turn.Events. This is
// intended for benchmarks that need to measure the event bus path
// (e.g., GuardStep success path publishing TurnStarted).
func newBenchTurnWithRealBus() *Turn {
	counter := &agenttest.MockTokenCounter{Tokens: 500}
	hMock := &agenttest.MockHistoryManager{
		Contents: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}},
	}
	bus := &eventstest.TestEventBus{}
	cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
	if err := cm.Reconfigure(events.Limits{
		MaxToolTurns:     100,
		MaxHistoryTokens: 1000000,
		MaxHistoryTurns:  100,
		ContextWindow:    1000000,
	}); err != nil {
		panic(err)
	}
	return &Turn{
		Index:        1,
		CtxManager:   cm,
		Events:       bus,
		TokenCounter: counter,
		Clock:        &agenttest.MockClock{},
		State:        &TurnState{},
		Logger:       &ports.NoOpLogger{},
	}
}

// prewarmCache calls cm.Prepare once to populate the Manager's
// internal cache so that subsequent Prepare calls hit the cache.
func prewarmCache(tb testing.TB, cm *sessctx.Manager, turnIndex int) {
	tb.Helper()
	if _, _, err := cm.Prepare(context.Background(), turnIndex); err != nil {
		tb.Fatalf("prewarmCache: Prepare failed: %v", err)
	}
}

// BenchmarkScaffoldingSanity validates that the scaffolding compiles
// and runs correctly.
func BenchmarkScaffoldingSanity(b *testing.B) {
	turn := newBenchTurn()
	if turn.CtxManager == nil {
		b.Fatal("CtxManager is nil")
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = turn.CtxManager.GetLimits().MaxToolTurns
	}
}

// BenchmarkGuardStep measures the overhead of the Guard phase including
// limit validation and event publishing paths.
func BenchmarkGuardStep(b *testing.B) {
	b.Run("within_limit", func(b *testing.B) {
		step := &GuardStep{}
		turn := newBenchTurn()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
	})

	b.Run("exceeds_limit", func(b *testing.B) {
		step := &GuardStep{}
		turn := newBenchTurn()
		turn.Index = 101 // exceeds MaxToolTurns = 100
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
	})

	b.Run("with_real_bus", func(b *testing.B) {
		step := &GuardStep{}
		turn := newBenchTurnWithRealBus()
		bus := turn.Events.(*eventstest.TestEventBus)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
		b.StopTimer()
		if !bus.AssertEventPublished(reflect.TypeOf(events.TurnStarted{})) {
			b.Error("expected TurnStarted event to be published")
		}
	})
}

// BenchmarkContextRefiner measures the overhead of the context refinement
// phase including cache-hit and context-cancelled paths.
func BenchmarkContextRefiner(b *testing.B) {
	b.Run("cache_hit", func(b *testing.B) {
		step := &ContextRefiner{}
		turn := newBenchTurn()
		prewarmCache(b, turn.CtxManager, turn.Index)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
		if turn.State.PreparedHistory == nil {
			b.Error("PreparedHistory is nil")
		}
		if turn.State.Metadata == nil {
			b.Error("Metadata is nil")
		}
	})

	b.Run("context_cancelled", func(b *testing.B) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		step := &ContextRefiner{}
		turn := newBenchTurn()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(ctx, turn)
		}
	})
}

// BenchmarkPersistenceStep measures the overhead of the Persistence phase
// including the no-response fast-path and the AddContent paths for
// response and tool-response content.
func BenchmarkPersistenceStep(b *testing.B) {
	b.Run("no_response", func(b *testing.B) {
		step := &PersistenceStep{}
		turn := newBenchTurn()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
		b.StopTimer()
		// Verify fast-path: both nil returns PhaseComplete
		res, _ := step.Process(context.Background(), turn)
		if res.NextPhase != PhaseComplete {
			b.Errorf("expected NextPhase=PhaseComplete, got %s", res.NextPhase)
		}
	})

	b.Run("with_response", func(b *testing.B) {
		step := &PersistenceStep{}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			turn := newBenchTurn()
			turn.State.Response = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
			b.StartTimer()
			_, _ = step.Process(context.Background(), turn)
		}
	})

	b.Run("with_tool_response", func(b *testing.B) {
		step := &PersistenceStep{}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			turn := newBenchTurn()
			turn.State.ToolResponse = &llm.Content{Role: "tool", Parts: []*llm.Part{{Text: "result"}}}
			b.StartTimer()
			_, _ = step.Process(context.Background(), turn)
		}
	})
}

// BenchmarkRecoveryStep measures the overhead of the Recovery phase
// including the no-error fast-path, max-retries failure path, and
// the full attemptRetry happy path with backoff.
func BenchmarkRecoveryStep(b *testing.B) {
	b.Run("no_error", func(b *testing.B) {
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
		turn := newBenchTurn()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
	})

	b.Run("max_retries_reached", func(b *testing.B) {
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
		turn := newBenchTurn()
		turn.State.LastError = llm.ErrTransient
		turn.State.RetryCount = 3
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = step.Process(context.Background(), turn)
		}
	})

	b.Run("retry_with_backoff", func(b *testing.B) {
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Millisecond}}
		turn := newBenchTurn()
		turn.State.LastError = llm.ErrTransient
		turn.State.RetryCount = 0
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			turn.State.RetryCount = 0
			_, _ = step.Process(context.Background(), turn)
		}
	})
}

// BenchmarkFullTurnCycle measures the cumulative per-turn cost of the
// phases that always execute in a normal turn (before inference and
// tool execution). It simulates the Engine.executePhase loop for a
// successful turn.
func BenchmarkFullTurnCycle(b *testing.B) {
	b.Run("happy_path", func(b *testing.B) {
		guard := &GuardStep{}
		refiner := &ContextRefiner{}
		persist := &PersistenceStep{}
		ctx := context.Background()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			turn := newBenchTurn()
			prewarmCache(b, turn.CtxManager, turn.Index)
			turn.State.Response = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
			b.StartTimer()
			_, _ = guard.Process(ctx, turn)
			_, _ = refiner.Process(ctx, turn)
			_, _ = persist.Process(ctx, turn)
		}
	})

	b.Run("with_recovery", func(b *testing.B) {
		guard := &GuardStep{}
		refiner := &ContextRefiner{}
		persist := &PersistenceStep{}
		recovery := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3, Backoff: 1 * time.Millisecond}}
		ctx := context.Background()

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			turn := newBenchTurn()
			prewarmCache(b, turn.CtxManager, turn.Index)
			turn.State.Response = &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}}
			turn.State.LastError = llm.ErrTransient
			turn.State.RetryCount = 0
			b.StartTimer()
			_, _ = guard.Process(ctx, turn)
			_, _ = refiner.Process(ctx, turn)
			_, _ = persist.Process(ctx, turn)
			_, _ = recovery.Process(ctx, turn)
		}
	})
}
