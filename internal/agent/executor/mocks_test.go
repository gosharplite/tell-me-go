// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package executor

import (
	"context"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockExecutor struct {
	Result tools.ToolResult
	Err    error
	Panic  bool
	Delay  time.Duration
	Called bool
}

func (m *mockExecutor) Execute(ctx context.Context, tool *tools.ToolDeclaration, call *llm.FunctionCall) (tools.ToolResult, error) {
	m.Called = true
	if m.Delay > 0 {
		select {
		case <-time.After(m.Delay):
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

type mockCircuitBreakerManager struct {
	CheckFunc  func(toolName string) error
	RecordFunc func(toolName string, success bool)
}

func (m *mockCircuitBreakerManager) Check(toolName string) error {
	if m.CheckFunc != nil {
		return m.CheckFunc(toolName)
	}
	return nil
}

func (m *mockCircuitBreakerManager) Record(toolName string, success bool) {
	if m.RecordFunc != nil {
		m.RecordFunc(toolName, success)
	}
}

type mockResolver struct {
	ResolveFunc func(call *llm.FunctionCall) (*tools.ToolDeclaration, error)
}

func (m *mockResolver) Resolve(call *llm.FunctionCall) (*tools.ToolDeclaration, error) {
	if m.ResolveFunc != nil {
		return m.ResolveFunc(call)
	}
	return &tools.ToolDeclaration{Name: call.Name}, nil
}

type mockEventBus struct {
	events.EventBus
	Published []events.Event
}

func (m *mockEventBus) Publish(ctx context.Context, e events.Event) error {
	m.Published = append(m.Published, e)
	return nil
}

func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {}
func (m *mockEventBus) Shutdown(ctx context.Context) error { return nil }
func (m *mockEventBus) Flush(ctx context.Context) error { return nil }
