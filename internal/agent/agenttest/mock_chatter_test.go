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
		chat, _, _, _, methods := m.Snapshot()
		if chat != 1 {
			t.Errorf("calledChat = %d; want 1", chat)
		}
		if len(methods) != 1 || methods[0] != "Chat" {
			t.Errorf("methods = %v; want [Chat]", methods)
		}
	})

	t.Run("delegates to ChatFn when set", func(t *testing.T) {
		wantErr := errors.New("chat failed")
		m := &MockChatter{
			ChatFn: func(ctx context.Context, s *ports.Session, prompt string) error {
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
		_, setLimits, _, _, methods := m.Snapshot()
		if setLimits != 1 {
			t.Errorf("calledSetLimits = %d; want 1", setLimits)
		}
		if len(methods) != 1 || methods[0] != "SetLimits" {
			t.Errorf("methods = %v; want [SetLimits]", methods)
		}
	})

	t.Run("delegates to SetLimitsFn when set", func(t *testing.T) {
		wantErr := errors.New("limits error")
		m := &MockChatter{
			SetLimitsFn: func(ctx context.Context, tt, ht, ht2 int) error {
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
	})
}

func TestMockChatter_Subscribe(t *testing.T) {
	t.Parallel()

	t.Run("no-op when SubscribeFn is nil", func(t *testing.T) {
		m := new(MockChatter)
		sub := func(ctx context.Context, ev events.Event) {}

		m.Subscribe(sub)
		_, _, subscribe, _, methods := m.Snapshot()
		if subscribe != 1 {
			t.Errorf("calledSubscribe = %d; want 1", subscribe)
		}
		if len(methods) != 1 || methods[0] != "Subscribe" {
			t.Errorf("methods = %v; want [Subscribe]", methods)
		}
	})

	t.Run("delegates to SubscribeFn with captured subscriber", func(t *testing.T) {
		var captured func(context.Context, events.Event)
		m := &MockChatter{
			SubscribeFn: func(sub func(context.Context, events.Event)) {
				captured = sub
			},
		}

		mySub := func(ctx context.Context, ev events.Event) {}
		m.Subscribe(mySub)

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
		_, _, _, shutdown, methods := m.Snapshot()
		if shutdown != 1 {
			t.Errorf("calledShutdown = %d; want 1", shutdown)
		}
		if len(methods) != 1 || methods[0] != "Shutdown" {
			t.Errorf("methods = %v; want [Shutdown]", methods)
		}
	})

	t.Run("delegates to ShutdownFn when set", func(t *testing.T) {
		wantErr := errors.New("shutdown failed")
		m := &MockChatter{
			ShutdownFn: func(ctx context.Context) error {
				return wantErr
			},
		}

		err := m.Shutdown(ctx)
		if err != wantErr {
			t.Fatalf("got err=%v; want %v", err, wantErr)
		}
	})
}

func TestMockChatter_Snapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}

	m := new(MockChatter)

	// Perform various calls out of order.
	_ = m.Shutdown(ctx)
	_ = m.Chat(ctx, sess, "hi")
	_ = m.SetLimits(ctx, 1, 2, 3)
	m.Subscribe(func(ctx context.Context, ev events.Event) {})

	chat, setLimits, subscribe, shutdown, methods := m.Snapshot()
	if chat != 1 || setLimits != 1 || subscribe != 1 || shutdown != 1 {
		t.Errorf("counters = (%d,%d,%d,%d); want (1,1,1,1)",
			chat, setLimits, subscribe, shutdown)
	}
	want := []string{"Shutdown", "Chat", "SetLimits", "Subscribe"}
	if len(methods) != len(want) {
		t.Fatalf("methods len = %d; want %d (methods=%v)", len(methods), len(want), methods)
	}
	for i, m := range methods {
		if m != want[i] {
			t.Errorf("methods[%d] = %q; want %q", i, m, want[i])
		}
	}

	// Verify defensive copy: mutating the returned slice does not affect mock.
	methods[0] = "INJECTED"
	_, _, _, _, methods2 := m.Snapshot()
	if methods2[0] != "Shutdown" {
		t.Errorf("defensive copy failed: methods2[0] = %q; want Shutdown", methods2[0])
	}
}
