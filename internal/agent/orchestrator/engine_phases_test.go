// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestGuardStep_Process(t *testing.T) {
	t.Run("context cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		step := &GuardStep{}
		res, err := step.Process(ctx, &Turn{})

		assert.Equal(t, ProcessResult{}, res)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("turn exceeds limit", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
		if err := cm.Reconfigure(events.Limits{MaxToolTurns: 3}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

		turn := &Turn{
			Index:        5,
			Events:       bus,
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &GuardStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds limit")
		assert.ErrorIs(t, err, llm.ErrMaxTurnsReached)
	})

	t.Run("event bus not initialized", func(t *testing.T) {
		ctx := context.Background()

		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, nil, nil)
		if err := cm.Reconfigure(events.Limits{MaxToolTurns: 10}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

		turn := &Turn{
			Index:        1,
			Events:       nil, // not initialized
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &GuardStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{NextPhase: PhaseRefining}, res)
		assert.NoError(t, err)
	})

	t.Run("event publish failure (non-bus-init)", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("boom"))

		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)
		if err := cm.Reconfigure(events.Limits{MaxToolTurns: 10}); err != nil {
			t.Fatalf("Reconfigure setup failed: %v", err)
		}

		turn := &Turn{
			Index:        1,
			Events:       bus,
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &GuardStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "boom")
	})
}

func TestContextRefiner_Process(t *testing.T) {
	t.Run("context cancelled before prepare", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, nil, nil)

		turn := &Turn{
			Index:        0,
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &ContextRefiner{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("context preparation terminal error", func(t *testing.T) {
		ctx := context.Background()

		hMock := &agenttest.MockHistoryManager{}
		hMock.GetWindowErr = llm.ErrTerminal
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, nil, nil)

		turn := &Turn{
			Index:        0,
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &ContextRefiner{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context preparation failed")
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTerminal, agentErr.Category)
	})

	t.Run("context preparation transient error", func(t *testing.T) {
		ctx := context.Background()

		hMock := &agenttest.MockHistoryManager{}
		hMock.GetWindowErr = llm.ErrTransient
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, nil, nil)

		turn := &Turn{
			Index:        0,
			CtxManager:   cm,
			TokenCounter: counter,
			Clock:        &agenttest.MockClock{},
			State:        &TurnState{},
		}

		step := &ContextRefiner{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context preparation failed")
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTransient, agentErr.Category)
	})
}

func TestPersistenceStep_Process(t *testing.T) {
	t.Run("add content error on response", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
			return llm.ErrTerminal
		}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			CtxManager: cm,
			State: &TurnState{
				Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
			},
		}

		step := &PersistenceStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "history error")
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTerminal, agentErr.Category)
	})

	t.Run("add content error on tool response", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
			return errors.New("db down")
		}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			CtxManager: cm,
			State: &TurnState{
				Response:     nil,
				ToolResponse: &llm.Content{Role: "tool", Parts: []*llm.Part{{Text: "result"}}},
			},
		}

		step := &PersistenceStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to persist tool results")
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTerminal, agentErr.Category)
	})

	t.Run("transient error on response", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		hMock := &agenttest.MockHistoryManager{}
		hMock.Contents = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}
		hMock.AddContentFunc = func(ctx context.Context, content *llm.Content) error {
			return llm.ErrTransient
		}
		counter := &agenttest.MockTokenCounter{}
		cm := sessctx.NewManager(sessctx.NewStrategy(counter), hMock, bus, nil)

		turn := &Turn{
			CtxManager: cm,
			State: &TurnState{
				Response: &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}},
			},
		}

		step := &PersistenceStep{}
		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "history error")
		var agentErr *agentError
		assert.True(t, errors.As(err, &agentErr))
		assert.Equal(t, llm.ErrTransient, agentErr.Category)
	})
}

func TestRecoveryStep_Process(t *testing.T) {
	t.Run("no error — skips to complete", func(t *testing.T) {
		ctx := context.Background()

		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
		turn := &Turn{
			State: &TurnState{LastError: nil},
		}

		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{NextPhase: PhaseComplete}, res)
		assert.NoError(t, err)
	})

	t.Run("max retries reached on transient", func(t *testing.T) {
		ctx := context.Background()

		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
		turn := &Turn{
			Clock: &agenttest.MockClock{},
			State: &TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 3,
			},
		}

		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{NextPhase: PhaseComplete}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "max retries reached")
		assert.ErrorIs(t, err, llm.ErrTransient)
	})

	t.Run("non-transient error passes through", func(t *testing.T) {
		ctx := context.Background()

		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 3}}
		turn := &Turn{
			Clock: &agenttest.MockClock{},
			State: &TurnState{
				LastError:  llm.ErrTerminal,
				RetryCount: 0,
			},
		}

		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{NextPhase: PhaseComplete}, res)
		assert.ErrorIs(t, err, llm.ErrTerminal)
	})

	t.Run("context cancelled during retry wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		bus := &eventstest.TestEventBus{}
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 5, Backoff: 2 * time.Second}}
		turn := &Turn{
			Events: bus,
			Clock:  &agenttest.MockClock{},
			State: &TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 1,
			},
		}

		res, err := step.Process(ctx, turn)

		assert.Equal(t, ProcessResult{}, res)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("event publish failure (non-bus-init)", func(t *testing.T) {
		ctx := context.Background()

		bus := &eventstest.TestEventBus{}
		bus.SetPublishErr(errors.New("bus full"))

		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Second}}
		turn := &Turn{
			Events: bus,
			Clock:  &agenttest.MockClock{},
			State: &TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 0,
			},
		}

		res, err := step.Process(ctx, turn)

		// attemptRetry returns early after SafePublish failure (non-bus-init)
		assert.Equal(t, ProcessResult{}, res)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "bus full")
		// RetryCount was incremented at line 122 before the publish
		assert.Equal(t, 1, turn.State.RetryCount)
	})

	t.Run("context cancelled before select (interleaving)", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		bus := &eventstest.TestEventBus{} // no error injection — publish succeeds
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Second}}
		turn := &Turn{
			Events: bus,
			Clock:  &agenttest.MockClock{},
			State: &TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 0,
			},
		}

		res, err := step.Process(ctx, turn)

		// ctx.Err() check at line 147 catches the cancellation before select
		assert.Equal(t, ProcessResult{}, res)
		assert.ErrorIs(t, err, context.Canceled)
		// RetryCount incremented proves we passed through publish and hit
		// the ctx.Err() guard, not the select case
		assert.Equal(t, 1, turn.State.RetryCount)
	})

	t.Run("context cancelled during select wait", func(t *testing.T) {
		// This test covers the select { case <-ctx.Done(): ... } branch at line 154
		// in attemptRetry. Unlike the "cancelled during retry wait" and
		// "cancelled before select" tests which cancel before calling Process(),
		// this test cancels AFTER the goroutine has entered attemptRetry and
		// started blocking in the select.
		//
		// syncClock.After() closes calledCh on entry and returns an unbuffered
		// channel that never fires, forcing the select to block on ctx.Done().
		// The main goroutine waits on calledCh for a deterministic signal that
		// the spawned goroutine has reached the select boundary before calling cancel().
		bus := &passThroughEventBus{}
		clk := &syncClock{
			ch:       make(chan time.Time),
			calledCh: make(chan struct{}),
		}
		step := &RecoveryStep{Policy: &DefaultRetryPolicy{MaxRetries: 5, Backoff: 1 * time.Hour}}
		turn := &Turn{
			State: &TurnState{
				LastError:  llm.ErrTransient,
				RetryCount: 0,
			},
			Clock:  clk,
			Events: bus,
		}

		ctx, cancel := context.WithCancel(context.Background())
		// NOTE: cancel() is called below after the goroutine enters the select,
		// NOT here — the context must be alive when Process() starts.

		type procResult struct {
			res ProcessResult
			err error
		}
		resultCh := make(chan procResult, 1)

		go func() {
			res, err := step.Process(ctx, turn)
			resultCh <- procResult{res: res, err: err}
		}()

		// Block until the goroutine has entered the select (After() was called),
		// then cancel. This deterministically hits line 154: case <-ctx.Done().
		<-clk.calledCh
		cancel()
		r := <-resultCh

		assert.Equal(t, ProcessResult{}, r.res)
		assert.ErrorIs(t, r.err, context.Canceled)
		assert.Equal(t, 1, turn.State.RetryCount)
	})
}
