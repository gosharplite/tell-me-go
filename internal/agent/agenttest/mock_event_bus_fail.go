// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// MockEventBusFail is a test double for events.EventBus that always
// returns PublishErr from Publish. All other methods are no-ops. Use
// it to assert that callers correctly handle Publish failures.
type MockEventBusFail struct {
	PublishErr error
}

func (m *MockEventBusFail) Publish(ctx context.Context, e events.Event) error {
	return m.PublishErr
}
func (m *MockEventBusFail) Subscribe(f func(context.Context, events.Event)) {}
func (m *MockEventBusFail) Shutdown(ctx context.Context) error              { return nil }
func (m *MockEventBusFail) Flush(ctx context.Context) error                 { return nil }
func (m *MockEventBusFail) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }
func (m *MockEventBusFail) WaitStarted()                                    {}
