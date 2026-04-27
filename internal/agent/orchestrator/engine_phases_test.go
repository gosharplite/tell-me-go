// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestrator

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)
		cm.Reconfigure(events.Limits{MaxToolTurns: 3})

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, nil, nil)
		cm.Reconfigure(events.Limits{MaxToolTurns: 10})

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)
		cm.Reconfigure(events.Limits{MaxToolTurns: 10})

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, nil, nil)

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, nil, nil)

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, nil, nil)

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

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
		cm := session.NewContextManager(session.NewContextStrategy(counter), hMock, bus, nil)

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
}
