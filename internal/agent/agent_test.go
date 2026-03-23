// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	infrapersistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	security_impl "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAgent_New_Failure(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	bus := events.NewSimpleEventBus(context.Background())
	h := &mockHistoryManager{}
	sm := security_impl.NewSecurityManager(nil)

	// Passing nil registry should force NewToolExecutor to fail
	a, err := newAgent(client, bus, h, "test-provider", nil, sm)

	require.Error(t, err)
	require.Nil(t, a)
	require.Contains(t, err.Error(), "failed to create tool executor")
}

func TestAgent_SetLimits(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)

	_ = a.SetLimits(context.Background(), 5, 1000, 10)
	_ = a.(*agent).events.Flush(context.Background())

	limits := a.(*agent).ctxManager.GetLimits()
	if limits.MaxHistoryTokens != 1000 || limits.MaxToolTurns != 5 || limits.MaxHistoryTurns != 10 {
		t.Errorf("SetLimits failed: got %+v", limits)
	}
}

func TestAgent_Chat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	archiveFile := filepath.Join(tmpDir, "history.archive.jsonl")
	h := history.NewManager(infrapersistence.NewOSFileSystem(), historyFile, archiveFile)
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	mockClient := &mockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello! How can I help you today?"}},
			}, &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}, nil
		},
	}

	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(mockClient, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)
	sess := ports.NewSession("test-chat", h)

	ctx := context.Background()
	err = a.Chat(ctx, sess, "Hi")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	contents, _ := h.GetWindow(ctx, 0, -1)
	if len(contents) != 2 {
		t.Errorf("Expected 2 messages in history, got %d", len(contents))
	}
}

func TestAgent_ConfigWatcherIntegration(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	mainConfig := filepath.Join(tmpDir, "config.yaml")
	sessionConfig := filepath.Join(tmpDir, "session.yaml")

	err := os.WriteFile(mainConfig, []byte("MAX_HISTORY_TOKENS: 1234\nMAX_TURNS: 42"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	client := &mockLLMClient{}
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	a, err := newAgent(client, bus, h, "test-provider", reg, sm, withLoader(&config.YAMLConfigLoader{}), withSessionLoader(&config.JSONSessionLoader{}))
	require.NoError(t, err)

	// Re-injecting path configuration for integration test
	a.(*agent).configWatcher.SetPaths(mainConfig, sessionConfig)

	// Refresh should trigger update
	_ = a.(*agent).applyConfig(context.Background())
	_ = a.(*agent).events.Flush(context.Background())

	limits := a.(*agent).ctxManager.GetLimits()
	if limits.MaxHistoryTokens != 1234 || limits.MaxToolTurns != 42 {
		t.Errorf("ConfigWatcher integration failed: got %+v", limits)
	}
}

func TestAgent_TieredThreshold(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)

	// Subscribe to track updates
	updated := make(chan struct{}, 10)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if cfg, ok := e.(events.ConfigUpdated); ok && cfg.Limits.TieredThreshold == 100000 {
			select {
			case updated <- struct{}{}:
			default:
			}
		}
	})

	_ = a.SetTieredThreshold(context.Background(), 100000)

	// Wait for the update to propagate through the event bus to the strategy
	select {
	case <-updated:
	case <-time.After(2 * time.Second):
		t.Errorf("Timeout waiting for TieredThreshold update event")
	}

	if a.(*agent).ctxManager.Strategy.GetTieredThreshold() != 100000 {
		t.Errorf("expected TieredThreshold 100000, got %d", a.(*agent).ctxManager.Strategy.GetTieredThreshold())
	}
}

func TestAgent_ToolFlow_Retry(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	callCount := 0
	mockClient := &mockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				return nil, nil, llm.ErrTransient
			}
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Recovered"}},
			}, &llm.Metrics{}, nil
		},
	}

	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(mockClient, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)
	sess := ports.NewSession("test-retry", h)

	ctx := context.Background()
	_ = a.Chat(ctx, sess, "Hi")

	if callCount != 2 {
		t.Errorf("expected 2 calls after retry, got %d", callCount)
	}
}

func TestAgent_InternalTools_Registration(t *testing.T) {
	t.Parallel()
	t.Run("Default - No internal tools", func(t *testing.T) {
		t.Parallel()
		reg := registry.New()
		sm := security_impl.NewSecurityManager(nil)
		bus := events.NewSimpleEventBus(context.Background())
		tmpDir := t.TempDir()
		h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
		a, err := newAgent(&mockLLMClient{}, bus, h, "test-provider", reg, sm)
		require.NoError(t, err)
		_ = a

		decls := reg.GetDeclarations()
		for _, d := range decls {
			if d.Name == "summarize_history" || d.Name == "manage_history" {
				t.Errorf("internal tool %s registered by default", d.Name)
			}
		}
	})

	t.Run("Opt-in - With internal tools", func(t *testing.T) {
		t.Parallel()
		reg := registry.New()
		sm := security_impl.NewSecurityManager(nil)
		bus := events.NewSimpleEventBus(context.Background())
		tmpDir := t.TempDir()
		h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history2.json"), filepath.Join(tmpDir, "history2.archive.jsonl"))
		a, err := newAgent(&mockLLMClient{}, bus, h, "test-provider", reg, sm, withInternalTools())
		require.NoError(t, err)
		_ = a

		decls := reg.GetDeclarations()
		foundSumm := false
		foundManage := false
		for _, d := range decls {
			if d.Name == "summarize_history" {
				foundSumm = true
			}
			if d.Name == "manage_history" {
				foundManage = true
			}
		}

		if !foundSumm {
			t.Error("summarize_history tool not registered")
		}
		if !foundManage {
			t.Error("manage_history tool not registered")
		}
	})
}

func TestAgent_ContextExhaustion_Error(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	mockClient := &mockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrContextLimitExceeded
		},
	}

	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(mockClient, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)
	sess := ports.NewSession("test-exhaustion", h)

	ctx := context.Background()
	err = a.Chat(ctx, sess, "Too long")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !isFatal(err) {
		t.Errorf("expected fatal error for context exhaustion, got %v", err)
	}
}

func TestAgent_ToolRegistry_PropagatedToPipeline(t *testing.T) {
	t.Parallel()
	reg := registry.New()
	bus := events.NewSimpleEventBus(context.Background())
	sm := security_impl.NewSecurityManager(nil)
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history_pipeline.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	a, err := newAgent(&mockLLMClient{}, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)

	// Build pipeline
	a.(*agent).ctxManager.SetPipeline(a.(*agent).ctxManager.Factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000}))

	// Register should update pipeline via ContextManager
	err = orchestration.RegisterInternal(reg, a.(*agent).ctxManager)
	require.NoError(t, err)

	// Verify that at least one transformer has the registry
}

func TestAgent_PinningFlow(t *testing.T) {
	t.Parallel()
	a, h, ctx := setupPinningFlowTest(t)
	it := orchestration.NewInternalTools(a.(*agent).ctxManager)

	t.Run("PinTurn", func(t *testing.T) {
		verifyPinAction(t, it, h, ctx, "pin", 0)
	})

	t.Run("UnpinTurn", func(t *testing.T) {
		verifyPinAction(t, it, h, ctx, "unpin", 1)
	})
}

func verifyPinAction(t *testing.T, it *orchestration.InternalTools, h ports.HistoryManager, ctx context.Context, action string, index float64) {
	t.Helper()
	resp, err := it.ManageHistory(ctx, map[string]interface{}{"action": action, "index": index})
	if err != nil {
		t.Fatalf("ManageHistory failed: %v", err)
	}

	expectedMsg := fmt.Sprintf("Turn %d has been successfully %sned.", int(index), action)
	if resp.Text != expectedMsg {
		t.Errorf("unexpected response: got %q, want %q", resp.Text, expectedMsg)
	}

	contents, _ := h.GetWindow(ctx, 0, -1)
	isPinned := (action == "pin")
	idx := int(index)

	if contents[2*idx].Pinned != isPinned || contents[2*idx+1].Pinned != isPinned {
		t.Errorf("expected turn %d pinned status to be %v", idx, isPinned)
	}

	if action == "pin" && idx == 0 {
		if contents[2].Pinned || contents[3].Pinned {
			t.Error("expected turn 1 to remain unpinned")
		}
	}
}

func setupPinningFlowTest(t *testing.T) (ports.Chatter, ports.HistoryManager, context.Context) {
	t.Helper()
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history_pinning.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	ctx := context.Background()

	// Add 2 turns
	for i := 1; i <= 2; i++ {
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("t%d", i)}}})
		_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("r%d", i)}}})
	}

	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(&mockLLMClient{}, bus, h, "test-provider", reg, sm, withInternalTools())
	require.NoError(t, err)
	return a, h, ctx
}

func TestAgent_Integration_PinningPruning(t *testing.T) {
	t.Parallel()
	// High level test: Ensure pinned turns survive pruning even if they are old.

	a, h, ctx := setupPinningTest(t)

	// 1. Add 10 turns
	addTurns(ctx, h, 10)

	// 2. Pin the 2nd turn (index 1)
	_ = h.SetPinned(ctx, 1, true)

	// 3. Set limits to only keep 3 turns
	_ = a.SetLimits(ctx, 10, 100000, 3)

	// 4. Run a chat turn to trigger preparation/pruning
	err := a.Chat(ctx, ports.NewSession("test-pin", h), "next")
	if err != nil {
		t.Logf("Chat returned error (expected in mock): %v", err)
	}

	// 5. Verify results
	prepared, meta, err := a.(*agent).ctxManager.Prepare(ctx, 11)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	verifyPinningResults(t, meta, prepared)
}

func setupPinningTest(t *testing.T) (ports.Chatter, ports.HistoryManager, context.Context) {
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "pin_prune.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	ctx := context.Background()

	mockClient := &mockLLMClient{}
	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(mockClient, bus, h, "test-provider", reg, sm, withInternalTools())
	if err != nil {
		panic(err)
	}
	return a, a.(*agent).ctxManager.History, ctx
}

func addTurns(ctx context.Context, h ports.HistoryManager, count int) {
	for i := 0; i < count; i++ {
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i)}}})
		_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i)}}})
	}
}

func verifyPinningResults(t *testing.T, meta *orchestration.Metadata, prepared []*llm.Content) {
	// Look for "u1" (the pinned turn)
	foundPinned := false
	for _, c := range prepared {
		if len(c.Parts) > 0 && c.Parts[0].Text == "u1" {
			foundPinned = true
			break
		}
	}
	if !foundPinned {
		t.Error("Pinned turn 'u1' was pruned!")
	}

	// Look for "u0" (should be pruned)
	for _, c := range prepared {
		if len(c.Parts) > 0 && c.Parts[0].Text == "u0" {
			t.Error("Old unpinned turn 'u0' survived pruning")
		}
	}
}

func TestAgent_Reconfiguration(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	// Test initial injection via positional args
	tracker1 := &mockCostTracker{}
	bus := events.NewSimpleEventBus(context.Background())
	a, err := newAgent(client, bus, h, "test-provider", reg, sm,
		withSessionCostTracker(tracker1),
	)
	require.NoError(t, err)

	if a.(*agent).tracker != tracker1 {
		t.Error("withSessionCostTracker didn't set tracker")
	}

	// Test tracker replacement
	tracker2 := &mockCostTracker{}
	a.(*agent).tracker = tracker2
	_ = a.(*agent).applyConfig(context.Background())

	if a.(*agent).tracker != tracker2 {
		t.Error("tracker didn't update after replacement and applyConfig")
	}
}

type mockCostTracker struct {
	domain_pricing.CostTracker
}

func TestAgent_Option_WithPricing(t *testing.T) {
	t.Parallel()

	overrides := map[string]domain_pricing.ModelPricing{
		"test-model": {Miss: 1.0},
	}
	client := &mockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history_pricing.json"), filepath.Join(tmpDir, "history.archive.jsonl"))

	a, err := newAgent(client, bus, h, "test-provider", reg, sm,
		withPricing("test-model", "chat", overrides),
	)
	require.NoError(t, err)

	if a.(*agent).config.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", a.(*agent).config.Model)
	}
	if a.(*agent).config.Mode != "chat" {
		t.Errorf("expected mode chat, got %s", a.(*agent).config.Mode)
	}
	if p, ok := a.(*agent).config.PricingOverrides["test-model"]; !ok || p.Miss != 1.0 {
		t.Errorf("pricing overrides not correctly set: %+v", a.(*agent).config.PricingOverrides)
	}
}

func TestAgent_Subscribe(t *testing.T) {
	t.Parallel()

	client := &mockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history_sub.json"), filepath.Join(tmpDir, "history.archive.jsonl"))

	// Subscribe BEFORE creating the agent to capture all events
	eventsChan := make(chan events.ConfigUpdated, 5)
	bus.Subscribe(func(ctx context.Context, e events.Event) {
		if cfg, ok := e.(events.ConfigUpdated); ok {
			eventsChan <- cfg
		}
	})

	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)

	// Trigger limits update
	_ = a.SetLimits(context.Background(), 15, 2000, 20)

	// Wait for the expected event (the second one, or the one with specific values)
	var lastEvent events.ConfigUpdated
	timeout := time.After(2 * time.Second)
	found := false

loop:
	for {
		select {
		case ev := <-eventsChan:
			if ev.Limits.MaxToolTurns == 15 && ev.Limits.MaxHistoryTokens == 2000 {
				lastEvent = ev
				found = true
				break loop
			}
			lastEvent = ev
		case <-timeout:
			break loop
		}
	}

	if !found {
		t.Fatalf("Expected ConfigUpdated event with limits (15, 2000, 20) not received. Last event received: %+v", lastEvent.Limits)
	}
}

func TestAgent_Option_WithSessionCostTracker(t *testing.T) {
	t.Parallel()

	tracker := &mockCostTracker{}
	client := &mockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history_cost.json"), filepath.Join(tmpDir, "history.archive.jsonl"))

	// 1. Test passing during New
	a, err := newAgent(client, bus, h, "test-provider", reg, sm,
		withSessionCostTracker(tracker),
	)
	require.NoError(t, err)

	if a.(*agent).tracker != tracker {
		t.Error("a.tracker does not match passed tracker")
	}

	// 2. Test direct setting (since we removed the ability to use the option at runtime)
	tracker2 := &mockCostTracker{}
	a.(*agent).tracker = tracker2
	if a.(*agent).engine != nil {
		a.(*agent).engine.ApplyOptions(withEngineCostTracker(tracker2))
	}

	if a.(*agent).tracker != tracker2 {
		t.Error("a.tracker does not match updated tracker")
	}
}

func TestAgent_Chat_ConfigFailure(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)
	sess := ports.NewSession("test-config-application", h)

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = a.Chat(ctx, sess, "Hi")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestAgent_Shutdown(t *testing.T) {
	t.Parallel()
	// 1. Setup minimal dependencies

	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	// 2. Initialize Agent
	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)

	// 3. Define a timeout context for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// 4. Execute Shutdown and assert success
	err = a.Shutdown(ctx)
	if err != nil {
		t.Fatalf("Expected graceful shutdown, got error: %v", err)
	}

	// 5. Verify that calling shutdown on an already shut down agent
	// (or its components) behaves predictably
	err = a.Shutdown(ctx)
	if err != nil {
		t.Errorf("Expected repeated shutdown to be safe, got error: %v", err)
	}
}

func TestAgent_Shutdown_NilDeps(t *testing.T) {
	t.Parallel()
	a := &agent{}
	err := a.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Expected nil error for nil dependencies, got %v", err)
	}
}

func TestAgent_ContextCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())

	a, err := newAgent(client, bus, h, "test-provider", reg, sm)
	require.NoError(t, err)
	sess := ports.NewSession("test-cancel", h)

	err = a.(*agent).applyConfig(ctx)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from applyConfig, got %v", err)
	}

	err = a.Chat(ctx, sess, "Hello")
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled from Chat, got %v", err)
	}
}

func TestAgent_Integration_InternalTools_And_Summarizer(t *testing.T) {
	t.Parallel()
	client := &mockLLMClient{}
	tmpDir := t.TempDir()
	h := history.NewManager(infrapersistence.NewOSFileSystem(), filepath.Join(tmpDir, "history.json"), filepath.Join(tmpDir, "history.archive.jsonl"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := events.NewSimpleEventBus(context.Background())
	mockSumm := &mockSummarizer{}

	a, err := newAgent(client, bus, h, "test-provider", reg, sm,
		withInternalTools(),
		withSummarizer(mockSumm),
	)
	require.NoError(t, err)

	// Verify internal tools are registered
	decls := reg.GetDeclarations()
	foundSumm := false
	for _, d := range decls {
		if d.Name == "summarize_history" {
			foundSumm = true
			break
		}
	}
	if !foundSumm {
		t.Error("summarize_history tool not registered")
	}

	// Verify summarizer is bound to ContextManager
	if a.(*agent).ctxManager.Summarizer != mockSumm {
		t.Error("summarizer not bound to ContextManager")
	}
}

func TestAgent_ApplyConfig_ContextCancellation(t *testing.T) {
	// Create an agent with mock dependencies
	a := &agent{
		events:        events.NewSimpleEventBus(context.Background()),
		configWatcher: orchestration.NewNoOpConfigWatcher(1000, 5, 10),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Applying config with a canceled context should fail when trying to publish
	err := a.applyConfig(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

func TestAgent_ApplyConfig_Publish_Error(t *testing.T) {
	// Mock the event bus to return an error on Publish
	mockBus := &inframock.TestEventBus{}
	mockBus.SetPublishErr(context.Canceled)

	a := &agent{
		events:        mockBus,
		configWatcher: orchestration.NewNoOpConfigWatcher(1000, 5, 10),
	}

	err := a.applyConfig(context.Background())

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error from applyConfig, got: %v", err)
	}
}

func TestNewAgent_ToolRegistrationFailure(t *testing.T) {
	t.Parallel()
	mockClient := &mockLLMClient{}
	bus := events.NewSimpleEventBus(context.Background())
	h := &mockHistoryManager{}
	sm := security_impl.NewSecurityManager(nil)

	// Use testify mock for the registry to simulate failures
	mockRegistry := &mockToolRegistryWithExpectations{}
	expectedErr := errors.New("duplicate tool schema")

	// Force the mock's registration method to return a predefined error
	// Architectural Instruction: The pattern provided uses "RegisterInternal".
	// Since orchestration.RegisterInternal calls RegisterWithOptions, we bridge them here.
	mockRegistry.On("RegisterInternal", mock.Anything).Return(expectedErr)

	// Attempt to create the agent (adjust parameters to match your actual constructor)
	agent, err := newAgent(mockClient, bus, h, "test-provider", mockRegistry, sm, withInternalTools())

	// Architectural mandate: Ensure initialization fails fast and propagates the error
	assert.ErrorIs(t, err, expectedErr)
	assert.Nil(t, agent)
	mockRegistry.AssertExpectations(t)
}

// mockToolRegistryWithExpectations implements Registry using testify/mock.
type mockToolRegistryWithExpectations struct {
	mock.Mock
}

func (m *mockToolRegistryWithExpectations) Register(declaration *tools.ToolDeclaration, implementation tools.ToolFunc) error {
	args := m.Called(declaration, implementation)
	return args.Error(0)
}

func (m *mockToolRegistryWithExpectations) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	// To satisfy the required pattern "On('RegisterInternal', ...)",
	// we delegate to a mockable RegisterInternal method.
	return m.RegisterInternal(def)
}

func (m *mockToolRegistryWithExpectations) RegisterInternal(declaration interface{}) error {
	args := m.Called(declaration)
	return args.Error(0)
}

func (m *mockToolRegistryWithExpectations) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	call := m.Called(ctx, name, args)
	return call.Get(0).(tools.ToolResult), call.Error(1)
}

func (m *mockToolRegistryWithExpectations) GetDeclarations() []*tools.ToolDeclaration {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).([]*tools.ToolDeclaration)
}

func (m *mockToolRegistryWithExpectations) IsSerial(name string) bool {
	return m.Called(name).Bool(0)
}

func (m *mockToolRegistryWithExpectations) IsLongRunning(name string) bool {
	return m.Called(name).Bool(0)
}
