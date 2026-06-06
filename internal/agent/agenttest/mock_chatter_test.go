// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/stretchr/testify/mock"
)

func TestMockChatter_Chat(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sess := &ports.Session{ID: "s1"}

	m := new(MockChatter)
	m.On("Chat", ctx, sess, "hello").Return(nil)

	err := m.Chat(ctx, sess, "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockChatter_SetLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockChatter)
	m.On("SetLimits", ctx, 5, 1000, 10).Return(nil)

	err := m.SetLimits(ctx, 5, 1000, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}

func TestMockChatter_Subscribe(t *testing.T) {
	t.Parallel()

	sub := func(ctx context.Context, ev events.Event) {}

	m := new(MockChatter)
	m.On("Subscribe", mock.Anything).Return()

	m.Subscribe(sub)
	m.AssertCalled(t, "Subscribe", mock.Anything)
}

func TestMockChatter_Shutdown(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	m := new(MockChatter)
	m.On("Shutdown", ctx).Return(nil)

	err := m.Shutdown(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m.AssertExpectations(t)
}
