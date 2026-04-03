// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockExecutor struct {
	mu          sync.Mutex
	Result      tools.ToolResult
	Err         error
	Panic       bool
	Delay       time.Duration
	Called      bool
	BlockCh     chan struct{}
	ExecuteHook func()
}

func (m *mockExecutor) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall, hb chan<- struct{}) (tools.ToolResult, error) {
	m.mu.Lock()
	m.Called = true
	m.mu.Unlock()

	if m.ExecuteHook != nil {
		m.ExecuteHook()
	}

	if m.Delay > 0 {
		select {
		case <-m.BlockCh:
		case <-ctx.Done():
			return tools.ToolResult{}, ctx.Err()
		}
	}
	if m.Panic {
		panic("mock panic")
	}
	return m.Result, m.Err
}

type mockToolAuthService struct {
	AuthorizeFunc func(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error
}

func (m *mockToolAuthService) Authorize(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) error {
	if m.AuthorizeFunc != nil {
		return m.AuthorizeFunc(ctx, tool, call)
	}
	return nil
}

type mockEventBus struct {
	events.EventBus
	mu        sync.Mutex
	Published []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Published = append(m.Published, e)
	return nil
}

func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (m *mockEventBus) Shutdown(ctx context.Context) error                { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error                   { return nil }
