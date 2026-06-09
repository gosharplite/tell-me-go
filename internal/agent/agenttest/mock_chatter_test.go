// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestMockChatter_Chat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}

	t.Run("default success when ChatFn is nil", func(t *testing.T) {
		m := new(MockChatter)

		err := m.Chat(ctx, sess, "hello")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("delegates to ChatFn when set", func(t *testing.T) {
		var called bool
		wantErr := errors.New("chat failed")
		m := &MockChatter{
			ChatFn: func(ctx context.Context, s *ports.Session, prompt string) error {
				called = true
				if s.ID != "s1" || prompt != "hello" {
					t.Errorf("ChatFn got s.ID=%q, prompt=%q", s.ID, prompt)
				}
				return wantErr
			},
		}

		err := m.Chat(ctx, sess, "hello")
		if err != wantErr {
			t.Fatalf("got err=%v; want %v", err, wantErr)
		}
		if !called {
			t.Error("ChatFn was not called")
		}
	})
}

func TestMockChatter_SetLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("default success when SetLimitsFn is nil", func(t *testing.T) {
		m := new(MockChatter)

		err := m.SetLimits(ctx, 5, 1000, 10)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("delegates to SetLimitsFn when set", func(t *testing.T) {
		var called bool
		wantErr := errors.New("limits error")
		m := &MockChatter{
			SetLimitsFn: func(ctx context.Context, tt, ht, ht2 int) error {
				called = true
				if tt != 5 || ht != 1000 || ht2 != 10 {
					t.Errorf("SetLimitsFn got (%d,%d,%d)", tt, ht, ht2)
				}
				return wantErr
			},
		}

		err := m.SetLimits(ctx, 5, 1000, 10)
		if err != wantErr {
			t.Fatalf("got err=%v; want %v", err, wantErr)
		}
		if !called {
			t.Error("SetLimitsFn was not called")
		}
	})
}

func TestMockChatter_Subscribe(t *testing.T) {
	t.Parallel()

	t.Run("no-op when SubscribeFn is nil", func(t *testing.T) {
		m := new(MockChatter)
		sub := func(ctx context.Context, ev events.Event) {}

		// Must not panic
		m.Subscribe(sub)
	})

	t.Run("delegates to SubscribeFn with captured subscriber", func(t *testing.T) {
		var called bool
		var captured func(context.Context, events.Event)
		m := &MockChatter{
			SubscribeFn: func(sub func(context.Context, events.Event)) {
				called = true
				captured = sub
			},
		}

		mySub := func(ctx context.Context, ev events.Event) {}
		m.Subscribe(mySub)

		if !called {
			t.Error("SubscribeFn was not called")
		}
		if captured == nil {
			t.Fatal("captured subscriber is nil; SubscribeFn was not called")
		}
	})
}

func TestMockChatter_Shutdown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("default success when ShutdownFn is nil", func(t *testing.T) {
		m := new(MockChatter)

		err := m.Shutdown(ctx)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("delegates to ShutdownFn when set", func(t *testing.T) {
		var called bool
		wantErr := errors.New("shutdown failed")
		m := &MockChatter{
			ShutdownFn: func(ctx context.Context) error {
				called = true
				return wantErr
			},
		}

		err := m.Shutdown(ctx)
		if err != wantErr {
			t.Fatalf("got err=%v; want %v", err, wantErr)
		}
		if !called {
			t.Error("ShutdownFn was not called")
		}
	})
}
