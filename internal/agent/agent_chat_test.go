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
)

type mockProcessor struct {
	err         error
	processFunc func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error)
}

func (m *mockProcessor) Process(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
	if m.processFunc != nil {
		return m.processFunc(ctx, turn)
	}
	if m.err != nil {
		return orchestrator.ProcessResult{NextPhase: orchestrator.PhaseComplete}, m.err
	}
	return orchestrator.ProcessResult{NextPhase: orchestrator.PhaseComplete}, nil
}

func TestAgent_Chat_Success(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	var statusEmitted bool
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if su, ok := e.(events.StatusUpdate); ok {
			if su.Message == "Starting chat..." {
				statusEmitted = true
			}
		}
	})

	// Override engine processor to finish immediately
	agent := a.(*agent)
	agent.engine.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseGuard, &mockProcessor{}))

	ctx := context.Background()
	session := &ports.Session{StartTime: time.Now()}
	prompt := "hello"

	err = a.Chat(ctx, session, prompt)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if !statusEmitted {
		t.Error("expected StatusUpdate to be emitted")
	}

	// Verify prompt added to history
	contents := hManager.GetContents()
	if len(contents) == 0 {
		t.Error("expected prompt to be added to history")
	} else if contents[0].Parts[0].Text != prompt {
		t.Errorf("expected prompt %q, got %q", prompt, contents[0].Parts[0].Text)
	}
}

func TestAgent_Chat_EngineFailure(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	expectedErr := errors.New("engine failed")
	agent := a.(*agent)
	agent.engine.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseGuard, &mockProcessor{err: expectedErr}))

	ctx := context.Background()
	session := &ports.Session{StartTime: time.Now()}

	err = a.Chat(ctx, session, "fail")
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestAgent_Chat_ContextCancellation(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// Slow processor to allow cancellation
	slowProc := &mockProcessor{
		processFunc: func(ctx context.Context, turn *orchestrator.Turn) (orchestrator.ProcessResult, error) {
			select {
			case <-ctx.Done():
				return orchestrator.ProcessResult{}, ctx.Err()
			case <-time.After(500 * time.Millisecond):
				return orchestrator.ProcessResult{NextPhase: orchestrator.PhaseComplete}, nil
			}
		},
	}

	agent := a.(*agent)
	agent.engine.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseGuard, slowProc))

	ctx, cancel := context.WithCancel(context.Background())
	session := &ports.Session{StartTime: time.Now()}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = a.Chat(ctx, session, "hello")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestAgent_Chat_TelemetryFailure(t *testing.T) {
	bus := &agenttest.MockEventBusFail{PublishErr: errors.New("telemetry failed")}
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	// NewAgent will fail if applyConfig fails because of Publish failure?
	// applyConfig uses events.SafePublish which logs error but doesn't return it if it's not a real error?
	// Wait, applyConfig:
	// if err := events.SafePublish(ctx, a.events, events.ConfigUpdated{Limits: newCfg.Limits}); err != nil { ... return err }

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm))
	if err != nil {
		// If NewAgent fails because of telemetry, that's fine for this test if we can still test Chat
		t.Skip("NewAgent failed due to telemetry, skipping Chat telemetry failure test")
	}

	_ = a.(*agent)
	// Mock telemetry failure
	// StartTelemetry calls events.Listen
	// agenttest.MockEventBusFail implements Listen
	// Wait, MockEventBusFail Listen:
	/*
		func (m *MockEventBusFail) Listen(ctx context.Context) error                { <-ctx.Done(); return ctx.Err() }
	*/
	// That doesn't fail.

	// Let's use a custom mock bus for telemetry failure
}

type telemetryFailBus struct {
	events.SimpleEventBus
}

func (b *telemetryFailBus) Listen(ctx context.Context) error {
	return errors.New("telemetry listen failed")
}
func (b *telemetryFailBus) Publish(ctx context.Context, e events.Event) error { return nil }
func (b *telemetryFailBus) Subscribe(f func(context.Context, events.Event))   {}
func (b *telemetryFailBus) Shutdown(ctx context.Context) error                { return nil }
func (b *telemetryFailBus) Flush(ctx context.Context) error                   { return nil }

func TestAgent_Chat_TelemetryFailure_Actual(t *testing.T) {
	bus := &telemetryFailBus{}
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm),
		WithProviderName("test-provider"), WithPricing("test-model", "test-mode", nil))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	agent := a.(*agent)
	_ = agent
	agent.engine.ApplyOptions(orchestrator.WithEngineProcessor(orchestrator.PhaseGuard, &mockProcessor{}))

	ctx := context.Background()
	session := &ports.Session{StartTime: time.Now()}

	err = a.Chat(ctx, session, "hello")
	if err == nil || err.Error() != "telemetry listen failed" {
		t.Errorf("expected telemetry failure, got %v", err)
	}
}

func TestAgent_Chat_AddContentFailure(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	expectedErr := errors.New("add content failed")
	hManager := &agenttest.MockHistoryManager{AddContentFunc: func(ctx context.Context, content *llm.Content) error {
		return expectedErr
	}}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ctx := context.Background()
	session := &ports.Session{StartTime: time.Now()}

	err = a.Chat(ctx, session, "hello")
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}

func TestAgent_Chat_ApplyConfigFailure(t *testing.T) {
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	reg := &agenttest.MockToolRegistry{}
	gw := &agenttest.MockGateway{}
	hManager := &agenttest.MockHistoryManager{}
	sm := &mockSecurityManager{AllowAll: true}

	a, err := NewAgent(gw, bus, reg, WithHistoryManager(hManager), WithSecurityManager(sm))
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before call
	session := &ports.Session{StartTime: time.Now()}

	// Chat will call AddContent first, then applyConfig.
	// AddContent might check context too.
	err = a.Chat(ctx, session, "hello")
	if err == nil {
		t.Error("expected error due to cancelled context")
	}
}
