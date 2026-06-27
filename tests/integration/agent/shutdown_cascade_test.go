// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session/ui"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"

	"github.com/stretchr/testify/require"
)

// shutdownCascadeFixture holds all the wired dependencies needed to
// exercise the agent → bridge → event-bus shutdown cascade in tests.
type shutdownCascadeFixture struct {
	Agent       ports.Chatter
	Bridge      *ui.Bridge
	EventBus    events.EventBus
	History     ports.HistoryManager
	LLMClient   *agenttest.MockLLMClient
	Renderer    *agenttest.MockUIRenderer
	TurnsLogger *agenttest.MockTurnsLogger
	LogBuf      *bytes.Buffer
	TmpDir      string
	BusCancel   context.CancelFunc
	ListenDone  chan struct{} // closed when bridge.Listen() exits

	// TurnsLoggerCloseCount is incremented by the MockTurnsLogger.CloseFunc
	// closure. Tests assert it equals 1 after a graceful shutdown.
	TurnsLoggerCloseCount *atomic.Int32

	// Registry is the tool registry wired into the agent. Tests register
	// tools here before exercising Chat so that function-call responses
	// from the LLM mock resolve to real handlers.
	Registry tools.Registry
}

// setupShutdownCascadeTest wires a complete agent → bridge → event-bus
// stack backed by mocks and a real in-memory event bus (async mode).
//
// The returned fixture has all fields populated and t.Cleanup hooks
// registered for deterministic teardown: stop consumers first (bridge),
// then producers (bus).
func setupShutdownCascadeTest(t *testing.T) *shutdownCascadeFixture {
	t.Helper()

	// ---- Log capture ----
	logBuf := new(bytes.Buffer)
	slogLogger := slog.New(slog.NewTextHandler(logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// ---- Event bus (async, default) ----
	bus := events.NewSimpleEventBus(context.Background())

	busCtx, busCancel := context.WithCancel(context.Background())
	go func() {
		_ = bus.Listen(busCtx)
	}()
	bus.WaitStarted()

	// ---- Temp dir & history ----
	tmpDir := t.TempDir()
	hManager := history.NewManager(
		persistencetest.NewPlainOSFileSystem(),
		filepath.Join(tmpDir, "history.json"),
		filepath.Join(tmpDir, "history.archive.jsonl"),
	)

	// ---- Registry ----
	reg := registry.New()

	// ---- Mocks ----
	secMgr := &toolstest.MockSecurityManager{AllowAll: true}
	client := &agenttest.MockLLMClient{}
	renderer := &agenttest.MockUIRenderer{}
	clock := &agenttest.MockClock{}

	turnsLoggerCloseCount := new(atomic.Int32)
	turnsLogger := &agenttest.MockTurnsLogger{
		CloseFunc: func() error {
			turnsLoggerCloseCount.Add(1)
			return nil
		},
	}

	// ---- Agent ----
	chatAgent, err := agent.NewAgent(client, bus, reg,
		agent.WithHistoryManager(hManager),
		agent.WithProviderName("test-provider"),
		agent.WithPricing("test-model", "test-mode", nil),
		agent.WithSecurityManager(secMgr),
		agent.WithClock(clock),
		agent.WithTurnsLogger(turnsLogger),
		agent.WithLogger(slogLogger),
	)
	if err != nil {
		t.Fatalf("failed to create agent: %v", err)
	}

	// ---- Bridge ----
	// NOTE: withBridgeCleanupTimeout is unexported and cannot be set from
	// this external test package. The bridge defaults to a 5s cleanup
	// timeout, which is acceptable for integration tests.
	bridge := ui.NewBridge(renderer,
		ui.WithBridgeLogger(slogLogger),
		ui.WithBridgeClock(clock),
	)

	// Subscribe bridge to agent BEFORE starting bridge.Listen().
	// Pattern mirrors session_manager.go:setupUIRendering().
	chatAgent.Subscribe(func(ctx context.Context, e events.Event) {
		if err := bridge.HandleEvent(ctx, e); err != nil {
			if errors.Is(err, ui.ErrActorDead) {
				slogLogger.Warn("Bridge event failed: actor is dead", "error", err, "event", e.Type())
			} else if errors.Is(err, context.Canceled) {
				slogLogger.Debug("Bridge event skipped: context cancelled", "event", e.Type())
			} else {
				slogLogger.Warn("Failed to handle bridge event", "error", err, "event", e.Type())
			}
		}
	})

	// ---- Start bridge listener ----
	listenCtx, listenCancel := context.WithCancel(context.Background())
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		_ = bridge.Listen(listenCtx)
	}()
	bridge.WaitStarted()

	// ---- Fixture ----
	f := &shutdownCascadeFixture{
		Agent:                 chatAgent,
		Bridge:                bridge,
		EventBus:              bus,
		History:               hManager,
		LLMClient:             client,
		Renderer:              renderer,
		TurnsLogger:           turnsLogger,
		LogBuf:                logBuf,
		TmpDir:                tmpDir,
		BusCancel:             busCancel,
		ListenDone:            listenDone,
		TurnsLoggerCloseCount: turnsLoggerCloseCount,
		Registry:              reg,
	}

	// ---- Teardown hooks (reverse order: consumers first, then producers) ----
	t.Cleanup(func() {
		// 1. Stop bridge consumer: cancel Listen context
		listenCancel()
		// 2. Drain and clean up bridge
		f.Bridge.CloseInput()
		f.Bridge.Cleanup()
	})
	t.Cleanup(func() {
		// 3. Stop bus producer: cancel Listen context
		f.BusCancel()
		// 4. Graceful bus shutdown with 2s timeout
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = f.EventBus.Shutdown(shutdownCtx)
	})

	return f
}

func TestAgent_Shutdown_FullCascade(t *testing.T) {
	// ---- Phase 1: Setup ----
	f := setupShutdownCascadeTest(t)

	// Configure LLM mock: first call returns a tool-call, second returns text.
	var callCount int32
	f.LLMClient.SendChatFn = func(ctx context.Context, history []*llm.Content, toolDecls []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		n := atomic.AddInt32(&callCount, 1)
		if n == 1 {
			return &llm.Content{
				Role: "model",
				Parts: []*llm.Part{{
					FunctionCall: &llm.FunctionCall{Name: "echo", Args: map[string]interface{}{"message": "hello"}},
				}},
			}, &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}, nil
		}
		return &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Tool executed successfully."}},
		}, &llm.Metrics{PromptTokens: 20, ResponseTokens: 3}, nil
	}

	// Configure renderer to record spinner activity.
	f.Renderer.StartSpinnerWithStatusFn = func(ctx context.Context, status string) func() {
		return func() {} // no-op stop function
	}

	// Register the echo tool so the function call resolves to a real handler.
	echoDecl := &tools.ToolDeclaration{
		Name:        "echo",
		Description: "Echoes back the provided message.",
		Parameters: &tools.Schema{
			Type: "object",
			Properties: map[string]*tools.Schema{
				"message": {Type: "string", Description: "The message to echo back."},
			},
			Required: []string{"message"},
		},
	}
	echoHandler := tools.ToolFunc(func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
		msg, _ := args["message"].(string)
		return tools.ToolResult{Text: msg}, nil
	})
	if err := f.Registry.Register(echoDecl, echoHandler); err != nil {
		t.Fatalf("failed to register echo tool: %v", err)
	}

	// ---- Phase 2: Exercise ----
	sess := ports.NewSession("test-shutdown-cascade", f.History)
	err := f.Agent.Chat(context.Background(), sess, "Run the echo tool with message hello")
	if err != nil {
		t.Logf("Chat returned error (expected in mock mode): %v", err)
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	shutdownErr := f.Agent.Shutdown(shutdownCtx)

	// Mirror session_manager.go deferred cleanup: close input then drain/cleanup.
	f.Bridge.CloseInput()
	f.Bridge.Cleanup()

	// ---- Phase 3: Assertions ----

	// 1. Shutdown completes within timeout.
	require.NoError(t, shutdownErr, "Shutdown should complete within timeout")

	// 2. TurnsLogger.Close() was called exactly once.
	require.Equal(t, int32(1), f.TurnsLoggerCloseCount.Load(),
		"TurnsLogger.Close() should be called exactly once")

	// 3. EventBus reports ErrBusClosed (or context.Canceled) on Publish after shutdown.
	err = f.EventBus.Publish(context.Background(), events.StatusUpdate{
		Message: "post-shutdown", Level: "info",
	})
	require.True(t, errors.Is(err, events.ErrBusClosed) || errors.Is(err, context.Canceled),
		"Publish after shutdown should return ErrBusClosed or context.Canceled, got %v", err)

	// 4. Bridge input is closed (events are shed without error).
	err = f.Bridge.HandleEvent(context.Background(), events.StatusUpdate{
		Message: "post-shutdown-bridge", Level: "info",
	})
	require.NoError(t, err, "HandleEvent after bridge close should shed event without error")
	require.Contains(t, f.LogBuf.String(), "Shedding event: bridge is closed")

	// 5. Spinner was started during the chat turn.
	snap := f.Renderer.Snapshot()
	require.Greater(t, snap.StartSpinnerWithStatus, 0,
		"Spinner should have been started at least once during chat")

	// 6. History is flushed to disk (contents survive shutdown).
	historyPath := filepath.Join(f.TmpDir, "history.json")
	data, err := os.ReadFile(historyPath)
	require.NoError(t, err, "History file should exist after shutdown")
	require.NotEmpty(t, data, "History file should not be empty")
	require.Contains(t, string(data), "Run the echo tool",
		"History should contain the user prompt")

	// 7. Repeated Shutdown() is idempotent.
	secondCtx, secondCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer secondCancel()
	secondErr := f.Agent.Shutdown(secondCtx)
	require.NoError(t, secondErr, "Second Shutdown() should be idempotent and return nil")

	// ---- Phase 4: Wait for bridge goroutine to exit ----
	select {
	case <-f.ListenDone:
		// Expected: bridge goroutine exited.
	case <-time.After(1 * time.Second):
		t.Error("Bridge Listen() goroutine did not exit within 1s after Cleanup()")
	}
}
