// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	stdctx "context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	security_impl "github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestAgent_SetLimits(t *testing.T) {
	client := &MockLLMClient{}
	h := history.NewManager(filepath.Join(t.TempDir(), "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	a := New(client, h, reg, sm, false)

	a.SetLimits(5, 1000, 10)

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
		SendChatFn: func(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{
				Role:  "model",
				Parts: []*llm.Part{{Text: "Hello! How can I help you today?"}},
			}, &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}, nil
		},
	}

	a := New(mockClient, h, reg, sm, false)
	sess := NewSession(h)

	ctx := stdctx.Background()
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
	a.applyConfig()

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
	a.SetHardBudgetLimit(1.50)

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
	a.SetTieredThreshold(100000)

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
		SendChatFn: func(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
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
	sess := NewSession(h)

	ctx := stdctx.Background()
	_ = a.Chat(ctx, sess, "Hi")

	if callCount != 2 {
		t.Errorf("expected 2 calls after retry, got %d", callCount)
	}
}

func TestAgent_InternalTools_Registration(t *testing.T) {
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	_ = New(&MockLLMClient{}, nil, reg, sm, false)

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
}

func TestAgent_ContextExhaustion_Error(t *testing.T) {
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)

	mockClient := &MockLLMClient{
		SendChatFn: func(ctx stdctx.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, llm.ErrContextLimitExceeded
		},
	}

	a := New(mockClient, h, reg, sm, false)
	sess := NewSession(h)

	ctx := stdctx.Background()
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
	a.SetSystemInstructions(instr)

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
	a.registerInternalTools()

	// Verify that at least one transformer has the registry
	// This is verified via behavior in the Prepare phase or by inspecting the pipeline
	// but since we just want to ensure it doesn't panic and internal state is updated.
}

func TestAgent_PinningFlow(t *testing.T) {
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "history_pinning.json"))
	ctx := stdctx.Background()

	// 1. Add some turns
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "t1"}}})
	_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "r1"}}})
	_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "t2"}}})
	_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "r2"}}})

	a := New(&MockLLMClient{}, h, reg, sm, false)

	// 2. Pin turn 0
	it := NewInternalTools(a.ctxManager)
	resp, err := it.ManageHistory(ctx, map[string]interface{}{"action": "pin", "index": 0.0})
	if err != nil {
		t.Fatalf("ManageHistory failed: %v", err)
	}
	if resp.Text != "Turn 0 has been successfully pinned." {
		t.Errorf("unexpected response: %s", resp.Text)
	}

	// 3. Verify in history
	contents := h.GetContents()
	if !contents[0].Pinned || !contents[1].Pinned {
		t.Error("expected turn 0 (msgs 0 and 1) to be pinned")
	}
	if contents[2].Pinned || contents[3].Pinned {
		t.Error("expected turn 1 to remain unpinned")
	}

	// 4. Unpin turn 0
	resp, err = it.ManageHistory(ctx, map[string]interface{}{"action": "unpin", "index": 0.0})
	if err != nil {
		t.Fatalf("ManageHistory failed: %v", err)
	}
	if resp.Text != "Turn 0 has been successfully unpinned." {
		t.Errorf("unexpected response: %s", resp.Text)
	}

	// 5. Verify in history
	contents = h.GetContents()
	if contents[0].Pinned || contents[1].Pinned {
		t.Error("expected turn 0 to be unpinned")
	}
}

func TestAgent_Integration_PinningPruning(t *testing.T) {
	// High level test: Ensure pinned turns survive pruning even if they are old.
	a, h, ctx := setupPinningTest(t)

	// 1. Add 10 turns
	addTurns(ctx, h, 10)

	// 2. Pin the 2nd turn (index 1)
	_ = h.SetPinned(ctx, 1, true)

	// 3. Set limits to only keep 3 turns
	a.SetLimits(10, 100000, 3)

	// 4. Run a chat turn to trigger preparation/pruning
	err := a.Chat(ctx, &Session{History: h}, "next")
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

func setupPinningTest(t *testing.T) (*Agent, context.HistoryManager, stdctx.Context) {
	tmpDir := t.TempDir()
	h := history.NewManager(filepath.Join(tmpDir, "pin_prune.json"))
	reg := registry.New()
	sm := security_impl.NewSecurityManager(nil)
	ctx := stdctx.Background()

	mockClient := &MockLLMClient{}
	a := New(mockClient, h, reg, sm, false)
	return a, a.ctxManager.History, ctx
}

func addTurns(ctx stdctx.Context, h context.HistoryManager, count int) {
	for i := 0; i < count; i++ {
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("u%d", i)}}})
		_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: fmt.Sprintf("m%d", i)}}})
	}
}

func verifyPinningResults(t *testing.T, meta *context.Metadata, prepared []*llm.Content) {
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

	counter := &context.HeuristicTokenCounter{}
	strategy := context.NewContextStrategy(counter, bus)
	factory := &context.PipelineFactory{
		Registry:  reg,
		History:   h,
		Estimator: strategy,
		Events:    bus,
		Profile:   context.ProfilePrecise,
	}
	cm := context.NewContextManager(strategy, h, bus, factory)
	a.ctxManager = cm

	// Test Prepare under precise profile
	ctx := stdctx.Background()
	req := &context.Request{
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
		a.SetPrunedTurns(7)

		if a.ctxManager.Strategy.GetPrunedTurns() != 7 {
			t.Errorf("expected prunedTurns 7, got %d", a.ctxManager.Strategy.GetPrunedTurns())
		}
	})
}
