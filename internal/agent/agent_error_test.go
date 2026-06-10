// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agentinternal"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func TestAgent_ConfigFailure(t *testing.T) {
	t.Parallel()
	// Create a context that we can cancel

	ctx, cancel := context.WithCancel(context.Background())

	hm := &agenttest.MockHistoryManager{
		AddContentFunc: func(c context.Context, content *llm.Content) error {
			// Cancel context right after AddContent succeeds so applyConfig fails
			cancel()
			return nil
		},
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	// Construct a deliberately bare agent: no engine, no executor, no
	// configWatcher. We only set the two fields applyConfig and Chat
	// touch on the path under test, so cancelling ctx forces applyConfig
	// to return context.Canceled — the assertion below.
	a := agentinternal.NewBareAgent()
	a.SetEventsForTest(bus)
	a.SetCtxManagerForTest(&sessctx.Manager{
		History: hm,
	})

	sess := &ports.Session{StartTime: time.Now()}
	err := a.Chat(ctx, sess, "hello")

	if err == nil {
		t.Fatal("Expected error due to config failure/context cancellation, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled error from applyConfig, got: %v", err)
	}
}

// TestNewAgent_InitialConfigFailure_ReturnsError verifies that NewAgent
// returns an error (instead of swallowing it) when applyConfig fails
// during initial configuration application. This closes the gap where
// a partially-initialized agent was returned with only a non-blocking
// StatusUpdate warning event.
//
// The test uses WithInitContext with a cancelled context to force
// applyConfig to return context.Canceled. initComponents() succeeds
// because it does not consume initCtx, so the failure is isolated to
// the applyConfig call at the end of NewAgent.
func TestNewAgent_InitialConfigFailure_ReturnsError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel() // immediately cancel so applyConfig fails

	gw := &agenttest.MockGateway{}
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	eventstest.CleanupBus(t, bus)
	reg := agenttest.NewMockToolRegistry()
	sm := &agenttest.MockServiceSecurityManager{}

	_, err := agent.NewAgent(gw, bus, reg,
		agent.WithInitContext(cancelCtx),
		agent.WithSecurityManager(sm),
		agent.WithProviderName("test-provider"),
		agent.WithPricing("test-model", "test-mode", nil),
	)

	if err == nil {
		t.Fatal("expected error from NewAgent with cancelled init context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected error to wrap context.Canceled, got: %v", err)
	}
	// The error message must identify the source as "apply initial configuration"
	if !strings.Contains(err.Error(), "failed to apply initial configuration") {
		t.Errorf("expected error to contain 'failed to apply initial configuration', got: %v", err)
	}
}

// telemetryFailBus is a test double that succeeds on Publish (so NewAgent
// and applyConfig pass) but fails on Listen with a configurable error.
type telemetryFailBus struct {
	listenErr error
}

func (b *telemetryFailBus) Publish(ctx context.Context, e events.Event) error { return nil }
func (b *telemetryFailBus) Subscribe(f func(context.Context, events.Event))   {}
func (b *telemetryFailBus) Shutdown(ctx context.Context) error                { return nil }
func (b *telemetryFailBus) Flush(ctx context.Context) error                   { return nil }
func (b *telemetryFailBus) Listen(ctx context.Context) error                  { return b.listenErr }
func (b *telemetryFailBus) WaitStarted()                                      {}

// errTelemetryFailed and errOrchestrationFailed are sentinel errors used
// by TestChat_BothErrors_Collected to verify that errors.Join collects
// errors from both goroutines.
var (
	errTelemetryFailed     = errors.New("telemetry failed")
	errOrchestrationFailed = errors.New("orchestration failed")
)

// TestChat_BothErrors_Collected verifies the G7 fix: when both engine.Run
// and engine.StartTelemetry fail, errors.Join collects both errors instead
// of masking one behind the other (as errgroup did). It also verifies that
// when only one fails, the single error propagates correctly.
func TestChat_BothErrors_Collected(t *testing.T) {
	// NOTE: cannot use t.Parallel() because subtests use t.Setenv.

	t.Run("both errors collected", func(t *testing.T) {
		t.Setenv("TELL_ME_FAST_RETRY", "1")

		ctx := context.Background()
		gw := &agenttest.MockGateway{
			GenerateFunc: func(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return nil, nil, errOrchestrationFailed
			},
		}
		bus := &telemetryFailBus{listenErr: errTelemetryFailed}
		reg := agenttest.NewMockToolRegistry()
		hm := &agenttest.MockHistoryManager{}
		sm := &agenttest.MockServiceSecurityManager{}

		chatter, err := agent.NewAgent(gw, bus, reg,
			agent.WithHistoryManager(hm),
			agent.WithSecurityManager(sm),
			agent.WithProviderName("test-provider"),
			agent.WithPricing("test-model", "test-mode", nil),
		)
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}

		sess := &ports.Session{StartTime: time.Now()}
		err = chatter.Chat(ctx, sess, "hello")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "orchestration failed") {
			t.Errorf("expected error to contain 'orchestration failed', got: %v", err)
		}
		if !strings.Contains(err.Error(), "telemetry failed") {
			t.Errorf("expected error to contain 'telemetry failed', got: %v", err)
		}
	})

	t.Run("single error propagates", func(t *testing.T) {
		t.Setenv("TELL_ME_FAST_RETRY", "1")

		ctx := context.Background()
		// Default MockGateway returns success — Run completes without error.
		gw := &agenttest.MockGateway{}
		bus := &telemetryFailBus{listenErr: errTelemetryFailed}
		reg := agenttest.NewMockToolRegistry()
		hm := &agenttest.MockHistoryManager{}
		sm := &agenttest.MockServiceSecurityManager{}

		chatter, err := agent.NewAgent(gw, bus, reg,
			agent.WithHistoryManager(hm),
			agent.WithSecurityManager(sm),
			agent.WithProviderName("test-provider"),
			agent.WithPricing("test-model", "test-mode", nil),
		)
		if err != nil {
			t.Fatalf("NewAgent: %v", err)
		}

		sess := &ports.Session{StartTime: time.Now()}
		err = chatter.Chat(ctx, sess, "hello")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, errTelemetryFailed) {
			t.Errorf("expected errors.Is to find errTelemetryFailed, got: %v", err)
		}
	})
}
