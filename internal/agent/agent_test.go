// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_pricing "github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	security_impl "github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestAgent_SetLimits(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)

	_ = a.SetLimits(context.Background(), 5, 1000, 10)

	tokens, tools, historyTurns := a.ctxManager.Strategy.GetLimits()
	if tokens != 1000 || tools != 5 || historyTurns != 10 {
		t.Errorf("SetLimits failed: got tokens=%d, tools=%d, historyTurns=%d", tokens, tools, historyTurns)
	}
}

func TestAgent_Chat(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	h := history.NewManager(historyFile)
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	mockClient := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello! How can I help you today?"}},
			}, &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}, nil
		},
	}

	a := New(mockClient, h, reg, sm, false)
	sess := orchestration.NewSession("test-chat", h)

	ctx := context.Background()
	err := a.Chat(ctx, sess, "Hi")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	contents := h.GetContents()
	if len(contents) != 2 {
		t.Errorf("Expected 2 messages in history, got %d", len(contents))
	}
}

func TestAgent_Options(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false,
		WithLimits(3, 500, 5),
		WithSystemInstructions("Be helpful"),
	)

	tokens, tools, _ := a.ctxManager.Strategy.GetLimits()
	if tokens != 500 || tools != 3 {
		t.Errorf("WithLimits failed: got tokens=%d, tools=%d", tokens, tools)
	}
}

func TestAgent_ConfigWatcherIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	mainConfig := filepath.Join(tmpDir, "config.yaml")
	sessionConfig := filepath.Join(tmpDir, "session.yaml")

	err := os.WriteFile(mainConfig, []byte("MAX_HISTORY_TOKENS: 1234\nMAX_TURNS: 42"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)
	a.configWatcher.SetPaths(mainConfig, sessionConfig)

	// Refresh should trigger update
	_ = a.applyConfig(context.Background())

	tokens, tools, _ := a.ctxManager.Strategy.GetLimits()
	if tokens != 1234 || tools != 42 {
		t.Errorf("ConfigWatcher integration failed: got tokens=%d, tools=%d", tokens, tools)
	}
}

func TestAgent_BudgetLimit(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)
	_ = a.SetHardBudgetLimit(context.Background(), 1.50)

	if a.config.HardBudgetLimit != 1.50 {
		t.Errorf("expected HardBudgetLimit 1.50, got %.2f", a.config.HardBudgetLimit)
	}
	if a.engine.HardBudgetLimit != 1.50 {
		t.Errorf("expected Engine HardBudgetLimit 1.50, got %.2f", a.engine.HardBudgetLimit)
	}
}

func TestAgent_TieredThreshold(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)
	_ = a.SetTieredThreshold(context.Background(), 100000)

	if a.ctxManager.Strategy.GetTieredThreshold() != 100000 {
		t.Errorf("expected TieredThreshold 100000, got %d", a.ctxManager.Strategy.GetTieredThreshold())
	}
}

func TestAgent_ToolFlow_Retry(t *testing.T) {
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	callCount := 0
	mockClient := &MockLLMClient{
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

	a := New(mockClient, h, reg, sm, false)
	sess := orchestration.NewSession("test-retry", h)

	ctx := context.Background()
	_ = a.Chat(ctx, sess, "Hi")

	if callCount != 2 {
		t.Errorf("expected 2 calls after retry, got %d", callCount)
	}
}

func TestAgent_InternalTools_Registration(t *testing.T) {
	t.Run("Default - No internal tools", func(t *testing.T) {
		reg := registry.New()
		sm := security_impl.NewSecurityManager(nil)
		_ = New(&MockLLMClient{}, nil, reg, sm, false)

		decls := reg.GetDeclarations()
		for _, d := range decls {
			if d.Name == "summarize_history" || d.Name == "manage_history" {
				t.Errorf("internal tool %s registered by default", d.Name)
			}
		}
	})

	t.Run("Opt-in - With internal tools", func(t *testing.T) {
		reg := registry.New()
		sm := security_impl.NewSecurityManager(nil)
		_ = New(&MockLLMClient{}, nil, reg, sm, false, WithInternalTools())

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
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	mockClient := &MockLLMClient{
		SendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrContextLimitExceeded
		},
	}

	a := New(mockClient, h, reg, sm, false)
	sess := orchestration.NewSession("test-exhaustion", h)

	ctx := context.Background()
	err := a.Chat(ctx, sess, "Too long")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !IsFatal(err) {
		t.Errorf("expected fatal error for context exhaustion, got %v", err)
	}
}

func TestAgent_SystemInstructions_Sync(t *testing.T) {
	mockClient := &MockLLMClient{}
	a := New(mockClient, nil, registry.New(), security_impl.NewSecurityManager(nil), false)

	instr := "Act as a pirate"
	_ = a.SetSystemInstructions(context.Background(), instr)

	if a.config.SystemInstructions != instr {
		t.Errorf("expected instructions %q, got %q", instr, a.config.SystemInstructions)
	}
	// Gateway should have it too
	if a.gateway.GetSystemInstructions() != instr {
		t.Errorf("expected gateway instructions %q, got %q", instr, a.gateway.GetSystemInstructions())
	}
}

func TestAgent_ToolRegistry_PropagatedToPipeline(t *testing.T) {
	reg := registry.New()
	a := New(&MockLLMClient{}, nil, reg, security_impl.NewSecurityManager(nil), false)

	// Build pipeline
	a.ctxManager.SetPipeline(a.ctxManager.Factory.BuildStandardPipeline(events.Limits{MaxHistoryTokens: 1000}))

	// Register should update pipeline via ContextManager
	orchestration.RegisterInternal(reg, a.ctxManager)

	// Verify that at least one transformer has the registry
	// This is verified via behavior in the Prepare phase or by inspecting the pipeline
	// but since we just want to ensure it doesn't panic and internal state is updated.
}

func TestAgent_PinningFlow(t *testing.T) {
	a, h, ctx := setupPinningFlowTest(t)
	it := orchestration.NewInternalTools(a.ctxManager)

	t.Run("PinTurn", func(t *testing.T) {
		verifyPinAction(t, it, h, ctx, "pin", 0)
	})

	t.Run("UnpinTurn", func(t *testing.T) {
		verifyPinAction(t, it, h, ctx, "unpin", 0)
	})
}

func verifyPinAction(t *testing.T, it *orchestration.InternalTools, h services.HistoryManager, ctx context.Context, action string, index float64) {
	t.Helper()
	resp, err := it.ManageHistory(ctx, map[string]interface{}{"action": action, "index": index})
	if err != nil {
		t.Fatalf("ManageHistory failed: %v", err)
	}

	expectedMsg := fmt.Sprintf("Turn %d has been successfully %sned.", int(index), action)
	if resp.Text != expectedMsg {
		t.Errorf("unexpected response: got %q, want %q", resp.Text, expectedMsg)
	}

	contents := h.GetContents()
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

func setupPinningFlowTest(t *testing.T) (*Agent, services.HistoryManager, context.Context) {
	t.Helper()
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	h := history.NewManager(filepath.Join(t.TempDir(), "history_pinning.json"))
	ctx := context.Background()

	// Add 2 turns
	for i := 1; i <= 2; i++ {
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("t%d", i)}}})
		_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("r%d", i)}}})
	}

	a := New(&MockLLMClient{}, h, reg, sm, false, WithInternalTools())
	return a, h, ctx
}

func TestAgent_Integration_PinningPruning(t *testing.T) {
	// High level test: Ensure pinned turns survive pruning even if they are old.
	a, h, ctx := setupPinningTest(t)

	// 1. Add 10 turns
	addTurns(ctx, h, 10)

	// 2. Pin the 2nd turn (index 1)
	_ = h.SetPinned(ctx, 1, true)

	// 3. Set limits to only keep 3 turns
	_ = a.SetLimits(ctx, 10, 100000, 3)

	// 4. Run a chat turn to trigger preparation/pruning
	err := a.Chat(ctx, orchestration.NewSession("test-pin", h), "next")
	if err != nil {
		t.Logf("Chat returned error (expected in mock): %v", err)
	}

	// 5. Verify results
	// We expect:
	// - The most recent 3 turns (indices 7, 8, 9)
	// - The pinned 2nd turn (index 1)
	// - The new user prompt from Chat() (index 10)
	prepared, meta, err := a.ctxManager.Prepare(ctx, 11)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	verifyPinningResults(t, meta, prepared)
}

func setupPinningTest(t *testing.T) (*Agent, services.HistoryManager, context.Context) {
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "pin_prune.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	ctx := context.Background()

	mockClient := &MockLLMClient{}
	a := New(mockClient, h, reg, sm, false, WithInternalTools())
	return a, a.ctxManager.History, ctx
}

func addTurns(ctx context.Context, h services.HistoryManager, count int) {
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

func TestAgent_PreciseProfile_Sync(t *testing.T) {
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	bus := &events.SimpleEventBus{}
	mockClient := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))

	a := New(mockClient, h, reg, sm, false)
	a.events = bus

	counter := &orchestration.HeuristicTokenCounter{}
	strategy := orchestration.NewContextStrategy(counter, bus)
	factory := &orchestration.PipelineFactory{
		Registry:  reg,
		History:   h,
		Estimator: strategy,
		Events:    bus,
		Profile:   orchestration.ProfilePrecise,
	}
	cm := orchestration.NewContextManager(strategy, h, bus, factory)
	a.ctxManager = cm

	// Test Prepare under precise profile
	ctx := context.Background()
	req := &orchestration.Request{
		Turn:    1,
		History: []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "test"}}}},
	}
	pipeline := factory.BuildStandardPipeline(events.Limits{MaxHistoryTurns: 10, MaxHistoryTokens: 1000})

	// Verify that the pipeline was built (no panic)
	if pipeline == nil {
		t.Fatal("failed to build pipeline with precise profile")
	}

	err := pipeline.Execute(ctx, req)
	if err != nil {
		t.Errorf("pipeline execution failed: %v", err)
	}
}

func TestAgent_SetPrunedTurns(t *testing.T) {
	mockClient := &MockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	t.Run("SetPrunedTurns", func(t *testing.T) {
		a := New(mockClient, nil, reg, sm, false)
		_ = a.SetPrunedTurns(context.Background(), 7)

		if a.ctxManager.Strategy.GetPrunedTurns() != 7 {
			t.Errorf("expected prunedTurns 7, got %d", a.ctxManager.Strategy.GetPrunedTurns())
		}
	})
}

func TestAgent_Reconfiguration(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	// Test initial injection via option
	tracker1 := &MockCostTracker{}
	a := New(client, h, reg, sm, false,
		WithSessionCostTracker(tracker1),
	)

	if a.GetCostTracker() != tracker1 {
		t.Error("WithSessionCostTracker didn't set tracker")
	}
	if a.engine.GetCostTracker() != tracker1 {
		t.Error("engine didn't receive tracker from WithSessionCostTracker")
	}

	// Test runtime budget update
	_ = a.SetHardBudgetLimit(context.Background(), 2.50)
	if a.engine.HardBudgetLimit != 2.50 {
		t.Errorf("expected Engine HardBudgetLimit 2.50, got %.2f", a.engine.HardBudgetLimit)
	}

	// Test tracker replacement
	tracker2 := &MockCostTracker{}
	a.tracker = tracker2
	_ = a.applyConfig(context.Background())

	if a.GetCostTracker() != tracker2 {
		t.Error("GetCostTracker didn't return updated tracker")
	}
	if a.engine.GetCostTracker() != tracker2 {
		t.Error("engine didn't receive updated tracker after applyConfig")
	}
}

type MockCostTracker struct {
	domain_pricing.ICostTracker
}

func TestAgent_Option_WithPricing(t *testing.T) {
	t.Parallel()

	overrides := map[string]domain_pricing.ModelPricing{
		"test-model": {Miss: 1.0},
	}
	client := &MockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, nil, reg, sm, false,
		WithPricing("test-model", "chat", overrides),
	)

	if a.config.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", a.config.Model)
	}
	if a.config.Mode != "chat" {
		t.Errorf("expected mode chat, got %s", a.config.Mode)
	}
	if p, ok := a.config.PricingOverrides["test-model"]; !ok || p.Miss != 1.0 {
		t.Errorf("pricing overrides not correctly set: %+v", a.config.PricingOverrides)
	}
}

func TestAgent_Subscribe(t *testing.T) {
	t.Parallel()

	client := &MockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	a := New(client, nil, reg, sm, false)

	var eventReceived events.Event
	var mu sync.Mutex
	a.Subscribe(func(e events.Event) {
		mu.Lock()
		defer mu.Unlock()
		if _, ok := e.(events.ConfigUpdated); ok {
			eventReceived = e
		}
	})

	_ = a.SetLimits(context.Background(), 15, 2000, 20)

	mu.Lock()
	defer mu.Unlock()
	if eventReceived == nil {
		t.Fatal("ConfigUpdated event was not received")
	}

	cfgEvent := eventReceived.(events.ConfigUpdated)
	if cfgEvent.Limits.MaxToolTurns != 15 || cfgEvent.Limits.MaxHistoryTokens != 2000 || cfgEvent.Limits.MaxHistoryTurns != 20 {
		t.Errorf("unexpected limits in event: %+v", cfgEvent.Limits)
	}
}

func TestAgent_Option_WithSessionCostTracker(t *testing.T) {
	t.Parallel()

	tracker := &MockCostTracker{}
	client := &MockLLMClient{}
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	// 1. Test passing during New (engine is nil when option is applied)
	a := New(client, nil, reg, sm, false,
		WithSessionCostTracker(tracker),
	)

	if a.tracker != tracker {
		t.Error("a.tracker does not match passed tracker")
	}

	if a.GetCostTracker() != tracker {
		t.Error("a.GetCostTracker() does not return the passed tracker")
	}

	if a.engine.GetCostTracker() != tracker {
		t.Error("engine tracker does not match passed tracker")
	}

	// 2. Test applying to existing agent (engine is NOT nil)
	tracker2 := &MockCostTracker{}
	WithSessionCostTracker(tracker2)(a)

	if a.tracker != tracker2 {
		t.Error("a.tracker does not match updated tracker")
	}
	if a.engine.GetCostTracker() != tracker2 {
		t.Error("engine tracker does not match updated tracker")
	}
}

func TestAgent_Chat_ConfigFailure(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)
	sess := orchestration.NewSession("test-config-application", h)

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.Chat(ctx, sess, "Hi")
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}
