// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestrator"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/require"
)

func TestAgent_Chat_ConfigRefreshHook_Wiring(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg,
		WithHistoryManager(hManager),
		WithSecurityManager(sm),
		WithProviderName("test-provider"),
		WithPricing("test-model", "test-mode", nil),
	)
	require.NoError(t, err)

	accessor := AsInternal(chatter)
	require.NotNil(t, accessor, "AsInternal must return a non-nil accessor")

	ag := chatter.(*agent)
	ag.engine.ApplyOptions(orchestrator.WithEngineProcessor(
		orchestrator.PhaseGuard,
		&mockProcessor{},
	))

	spy := &spyHook{}
	ag.engine.ApplyOptions(orchestrator.WithEngineHook(spy))

	err = chatter.Chat(
		context.Background(),
		&ports.Session{StartTime: time.Now()},
		"hello",
	)
	require.NoError(t, err)

	require.GreaterOrEqual(t, spy.beforeCalls, 1, "BeforeTurn should be called at least once")
	require.GreaterOrEqual(t, spy.afterCalls, 1, "AfterTurn should be called at least once")
	require.NotEmpty(t, spy.transitions, "phase transitions should be recorded")
}

type spyHook struct {
	beforeCalls int
	afterCalls  int
	transitions []string
}

func (s *spyHook) BeforeTurn(turn *orchestrator.Turn) { s.beforeCalls++ }

func (s *spyHook) AfterTurn(turn *orchestrator.Turn, err error) { s.afterCalls++ }

func (s *spyHook) OnPhaseTransition(from, to orchestrator.TurnPhase, state *orchestrator.TurnState) {
	s.transitions = append(s.transitions, string(from)+"->"+string(to))
}

func TestAgent_Chat_ConfigRefreshHook_OnPhaseTransition(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}

	gw := &agenttest.MockGateway{
		GenerateFunc: func(ctx context.Context, input []*llm.Content, tl []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{FunctionCall: &llm.FunctionCall{Name: "test_tool"}}},
			}, &llm.Metrics{}, nil
		},
	}

	hManager := &mockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	chatter, err := NewAgent(gw, bus, reg,
		WithHistoryManager(hManager),
		WithSecurityManager(sm),
		WithProviderName("test-provider"),
		WithPricing("test-model", "test-mode", nil),
	)
	require.NoError(t, err)

	ag := chatter.(*agent)

	// Replace PhaseExecuting processor to avoid real tool execution.
	// The real ExecutionStep would try to invoke tools through the executor,
	// which isn't configured in this test.
	ag.engine.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseExecuting, &mockProcessor{
		processFunc: func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
			turn.State.ToolResponse = &llm.Content{Role: "tool", Parts: []*llm.Part{{Text: "mock tool result"}}}
			turn.State.HasToolCalls = false
			return orchestrator.ProcessResult{NextPhase: orchestrator.PhaseComplete}, nil
		},
	}))

	// Register spyHook before Chat
	spy := &spyHook{}
	ag.engine.ApplyOptions(orchestrator.WithEngineHook(spy))

	// Subscribe to ConfigUpdated events on the bus
	var configUpdatedReceived bool
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if _, ok := e.(events.ConfigUpdated); ok {
			configUpdatedReceived = true
		}
	})

	err = chatter.Chat(
		context.Background(),
		&ports.Session{StartTime: time.Now()},
		"hello",
	)
	require.NoError(t, err)

	require.GreaterOrEqual(t, spy.beforeCalls, 1, "BeforeTurn should be called at least once")
	require.GreaterOrEqual(t, spy.afterCalls, 1, "AfterTurn should be called at least once")
	require.Contains(t, spy.transitions, "Inference->Executing")
	require.True(t, configUpdatedReceived)
}

func TestConfigRefreshHook_BeforeTurn_NoOp(t *testing.T) {
	h := &configRefreshHook{}
	// Must not panic — BeforeTurn is an intentional no-op.
	h.BeforeTurn(nil)
}

func TestConfigRefreshHook_AfterTurn_NoOp(t *testing.T) {
	h := &configRefreshHook{}
	// Must not panic — AfterTurn is an intentional no-op, even with an error.
	h.AfterTurn(nil, nil)
	h.AfterTurn(nil, errors.New("some error"))
}
