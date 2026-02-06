// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/pricing"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestAgent_Setters(t *testing.T) {
	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()
	a := New(nil, nil, reg, sm, false)

	a.SetLimits(5, 1000, 20)
	maxTokens, maxTurns, maxHistTurns := a.strategy.GetLimits()
	if maxTurns != 5 || maxTokens != 1000 || maxHistTurns != 20 {
		t.Errorf("SetLimits failed: tokens=%d, toolTurns=%d, histTurns=%d", maxTokens, maxTurns, maxHistTurns)
	}

	a.SetConcurrency(10, 60)

	a.SetLogFile("test.log")
	if a.config.LogFile != "test.log" {
		t.Error("SetLogFile failed")
	}
}

func TestAgent_EstimateTokens(t *testing.T) {
	reg := registry.New()
	reg.Register(&tools.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  &tools.Schema{Type: "OBJECT"},
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "ok"}, nil
	})

	sm := &MockSecurityManager{AllowAll: true}
	a := New(nil, nil, reg, sm, false)

	contents := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "Hello world"},
				{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"a": 1}}},
				{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"res": "ok"}}},
			},
		},
	}

	tokens := a.strategy.EstimateTokens(contents)
	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}
}

func TestAgent_Chat_AuthRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	authCalls := 0
	mockClient := &MockLLMClient{
		RefreshAuthFn: func() error {
			authCalls++
			return nil
		},
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			if authCalls == 0 {
				return nil, fmt.Errorf("UNAUTHENTICATED")
			}
			callback(&llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Success"}}})
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	sess := NewSession(hManager)
	err := a.Chat(context.Background(), sess, "Hello")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if authCalls != 1 {
		t.Errorf("Expected 1 auth refresh call, got %d", authCalls)
	}
}

func TestAgent_Chat_ToolTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	// Tool that hangs
	reg.Register(&tools.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		time.Sleep(2 * time.Second)
		return tools.ToolResult{Text: "Too late"}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&llm.Content{Role: "model", Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{Name: "slow_tool"}},
				}})
			} else {
				callback(&llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Done"}}})
			}
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	a.SetConcurrency(5, 1) // 1 second timeout

	sess := NewSession(hManager)
	_ = a.Chat(context.Background(), sess, "Run slow tool")

	contents := hManager.GetContents()
	if len(contents) >= 3 {
		resp := contents[2].Parts[0].FunctionResponse.Response["result"].(string)
		if !strings.Contains(resp, "timed out") {
			t.Errorf("Expected timeout error message, got: %s", resp)
		}
	} else {
		t.Errorf("Expected at least 3 history entries, got %d", len(contents))
	}
}

func TestAgent_Chat_ImageInjection(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	reg.Register(&tools.ToolDeclaration{
		Name: "gen_image",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==")
		return tools.ToolResult{
			Text: "Image generated",
			BinaryData: []tools.BinaryData{
				{MIMEType: "image/png", Data: data},
			},
		}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&llm.Content{Role: "model", Parts: []*llm.Part{
					{FunctionCall: &llm.FunctionCall{Name: "gen_image"}},
				}})
			} else {
				callback(&llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Look at this"}}})
			}
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	sess := NewSession(hManager)
	_ = a.Chat(context.Background(), sess, "Generate an image")

	contents := hManager.GetContents()
	foundImage := false
	for _, c := range contents {
		for _, p := range c.Parts {
			if p.InlineData != nil && p.InlineData.MIMEType == "image/png" {
				foundImage = true
			}
		}
	}
	if !foundImage {
		t.Error("Image part not found in history after injection")
	}
}

func TestAgentToolLoop(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(historyFile)
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	// Register a dummy tool
	reg.Register(&tools.ToolDeclaration{
		Name:       "get_weather",
		Parameters: &tools.Schema{Type: "OBJECT"},
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "Sunny"}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&llm.Content{
					Role: "model",
					Parts: []*llm.Part{
						{Text: "I should check the weather.", Thought: true},
						{FunctionCall: &llm.FunctionCall{Name: "get_weather", Args: map[string]interface{}{}}},
					},
				})
			} else {
				callback(&llm.Content{
					Role:  "model",
					Parts: []*llm.Part{{Text: "It is sunny."}},
				})
			}
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)

	// Execute Chat
	sess := NewSession(hManager)
	err := a.Chat(context.Background(), sess, "What's the weather?")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Verify history sequence
	contents := hManager.GetContents()
	if len(contents) != 4 {
		t.Fatalf("Expected 4 history entries, got %d", len(contents))
	}
}

func TestAgent_Chat_MaxToolTurns(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	reg.Register(&tools.ToolDeclaration{
		Name: "infinite_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "Keep going"}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callCount++
			// Provide unique arguments to avoid the loop detector
			callback(&llm.Content{Role: "model", Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "infinite_tool", Args: map[string]interface{}{"n": callCount}}},
			}})
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	a.SetLimits(2, 2000, 20) // Max 2 turns

	sess := NewSession(hManager)
	err := a.Chat(context.Background(), sess, "Run tool")
	if !errors.Is(err, llm.ErrMaxTurnsReached) {
		t.Fatalf("Expected ErrMaxTurnsReached, got: %v", err)
	}
}

func TestAgent_Chat_APIError(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			return nil, fmt.Errorf("API Failure")
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	sess := NewSession(hManager)
	err := a.Chat(context.Background(), sess, "Hello")
	if err == nil {
		t.Error("Expected error on API failure, got nil")
	}
}

func TestAgent_Chat_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	// Tool that takes some time
	running := make(chan struct{})
	reg.Register(&tools.ToolDeclaration{
		Name: "long_tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		close(running)
		select {
		case <-time.After(1 * time.Second):
			return tools.ToolResult{Text: "Success"}, nil
		case <-ctx.Done():
			return tools.ToolResult{}, ctx.Err()
		}
	})

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callback(&llm.Content{Role: "model", Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "long_tool"}},
			}})
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)

	t.Run("CancelDuringToolExecution", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		// Cancel the context after the tool starts running
		go func() {
			select {
			case <-running:
				cancel()
			case <-time.After(500 * time.Millisecond):
				// Fallback to prevent hang if tool never starts
				cancel()
			}
		}()

		sess := NewSession(hManager)
		err := a.Chat(ctx, sess, "Run long tool")
		if err == nil {
			t.Error("Expected error due to context cancellation, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Expected error wrapping context.Canceled, got: %v", err)
		}
	})
}

func TestAgent_RefreshLimits(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()
	a := New(nil, nil, reg, sm, false)
	a.SetLimits(10, 1000, 20)

	// Set the config path
	a.SetPersistentConfigPath(configPath)

	// Create config file with string values
	configContent := `{"MAX_HISTORY_TOKENS": "5000", "MAX_TOOL_TURNS": "15"}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	a.applyConfig()

	maxTokens, maxTurns, _ := a.strategy.GetLimits()
	if maxTokens != 5000 {
		t.Errorf("expected maxHistoryTokens 5000, got %d", maxTokens)
	}
	if maxTurns != 15 {
		t.Errorf("expected maxToolTurns 15, got %d", maxTurns)
	}
}

func TestAgent_RefreshLimits_YAML(t *testing.T) {
	tmpDir := t.TempDir()
	yamlPath := filepath.Join(tmpDir, "config.yaml")

	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()
	a := New(nil, nil, reg, sm, false)
	a.SetLimits(10, 1000, 20)

	// Set the main config path
	a.SetMainConfigPath(yamlPath)

	// Create YAML config file
	yamlContent := "MAX_HISTORY_TOKENS: 200000\nMAX_TURNS: 25\nMAX_HISTORY_TURNS: 30"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml config file: %v", err)
	}

	a.applyConfig()

	maxTokens, maxTurns, maxHistTurns := a.strategy.GetLimits()
	if maxTokens != 200000 {
		t.Errorf("expected maxHistoryTokens 200000, got %d", maxTokens)
	}
	if maxTurns != 25 {
		t.Errorf("expected maxToolTurns 25, got %d", maxTurns)
	}
	if maxHistTurns != 30 {
		t.Errorf("expected maxHistoryTurns 30, got %d", maxHistTurns)
	}
}

func TestAgent_FunctionalOptions(t *testing.T) {
	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()

	a := New(nil, nil, reg, sm, false,
		WithLimits(5, 1000, 15),
		WithConcurrency(10, 60),
		WithLogFile("agent.log"),
		WithPersistentConfigPath("session.json"),
		WithMainConfigPath("config.yaml"),
	)

	if a.config.Limits.MaxToolTurns != 5 || a.config.Limits.MaxHistoryTokens != 1000 || a.config.Limits.MaxHistoryTurns != 15 {
		t.Errorf("WithLimits failed: %+v", a.config.Limits)
	}
	if a.config.Execution.MaxConcurrent != 10 || a.config.Execution.Timeout != 60*time.Second {
		t.Errorf("WithConcurrency failed: %+v", a.config.Execution)
	}
	if a.config.LogFile != "agent.log" {
		t.Errorf("WithLogFile failed: %s", a.config.LogFile)
	}
	if a.config.PersistentConfigPath != "session.json" {
		t.Errorf("WithPersistentConfigPath failed: %s", a.config.PersistentConfigPath)
	}
	if a.config.MainConfigPath != "config.yaml" {
		t.Errorf("WithMainConfigPath failed: %s", a.config.MainConfigPath)
	}
}

func TestAgent_SystemInstructions(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	var capturedInstr string
	mockClient := &MockLLMClient{
		SetSystemInstructionsFn: func(instr string) {
			capturedInstr = instr
		},
	}
	a := New(mockClient, hManager, reg, sm, false)

	customInstr := "You are a specialized Go expert."
	a.SetSystemInstructions(customInstr)

	if a.config.SystemInstructions != customInstr {
		t.Errorf("expected config instructions %q, got %q", customInstr, a.config.SystemInstructions)
	}

	if capturedInstr != customInstr {
		t.Errorf("expected captured instructions %q, got %q", customInstr, capturedInstr)
	}

	// Prepare history to trigger pipeline execution
	ctx := context.Background()
	_ = hManager.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})

	prepared, _, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// System instructions should NOT be in the prepared history anymore
	for _, c := range prepared {
		if c.Role == "user" && len(c.Parts) > 0 && strings.Contains(c.Parts[0].Text, customInstr) {
			t.Error("system instructions found in prepared history, but should be handled by the client")
		}
	}
}

func TestAgent_WithSystemInstructions(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	reg := registry.New()
	instr := "Initial instructions"

	a := New(&MockLLMClient{}, nil, reg, sm, false, WithSystemInstructions(instr))

	if a.config.SystemInstructions != instr {
		t.Errorf("expected config instructions %q, got %q", instr, a.config.SystemInstructions)
	}
}

func TestAgent_PinningIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	// Create agent with limit of 2 turns (4 messages)
	a := New(nil, hManager, reg, sm, false, WithLimits(10, 2000, 2))
	ctx := context.Background()

	// 1. Add 5 turns to history (10 messages)
	for i := 1; i <= 5; i++ {
		_ = hManager.AddContent(ctx, &llm.Content{
			Role:  "user",
			Parts: []*llm.Part{{Text: fmt.Sprintf("Turn %d User", i)}},
		})
		_ = hManager.AddContent(ctx, &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: fmt.Sprintf("Turn %d Model", i)}},
		})
	}

	// 2. Pin Turn 1 (messages 0 and 1)
	contents := hManager.GetContents()
	contents[0].Pinned = true // Pinning User message of Turn 1
	// contents[1] (Model) doesn't strictly need to be pinned as the turn-based policy handles pairs

	// 3. Trigger context preparation
	prepared, meta, err := a.ctxManager.Prepare(ctx, 1)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// 4. Verify results
	// Expected turns:
	// Turn 1 (Pinned)
	// Turn 4 (Window)
	// Turn 5 (Window)

	// Turn 2 and 3 should be pruned.

	// meta.PrunedTurns should be 2 (Turn 2 and Turn 3)
	if meta.PrunedTurns != 2 {
		t.Errorf("expected 2 pruned turns, got %d", meta.PrunedTurns)
	}

	// Check if Turn 1 is present
	foundT1 := false
	for _, c := range prepared {
		if strings.Contains(c.Parts[0].Text, "Turn 1 User") {
			foundT1 = true
			break
		}
	}
	if !foundT1 {
		t.Error("Turn 1 (Pinned) not found in prepared history")
	}

	// Check if Turn 2 is NOT present
	for _, c := range prepared {
		if strings.Contains(c.Parts[0].Text, "Turn 2 User") {
			t.Error("Turn 2 (Unpinned) found in prepared history, should have been pruned")
		}
	}

	// Check if Turn 5 is present (Last turn)
	foundT5 := false
	for _, c := range prepared {
		if strings.Contains(c.Parts[0].Text, "Turn 5 User") {
			foundT5 = true
			break
		}
	}
	if !foundT5 {
		t.Error("Turn 5 (Window) not found in prepared history")
	}
}

func TestToolInjection_NoPersistence(t *testing.T) {
	tmpDir := t.TempDir()
	historyFile := filepath.Join(tmpDir, "history.jsonl")
	hManager := history.NewManager(historyFile)
	reg := registry.New()
	sm := &MockSecurityManager{AllowAll: true}

	// Register a tool
	reg.Register(&tools.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
	}, func(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
		return tools.ToolResult{Text: "ok"}, nil
	})

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver, callback func(*llm.Content)) (*llm.Metrics, error) {
			callback(&llm.Content{Role: "model", Parts: []*llm.Part{{Text: "Done"}}})
			return &llm.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, reg, sm, false)
	sess := NewSession(hManager)

	err := a.Chat(context.Background(), sess, "Hello")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	// Read the history file
	data, err := os.ReadFile(historyFile)
	if err != nil {
		t.Fatalf("failed to read history file: %v", err)
	}

	if strings.Contains(string(data), "# AVAILABLE_TOOLS") {
		t.Error("History file contains tool schemas (# AVAILABLE_TOOLS), but they should be transient!")
	}
}

func TestOptimizationProfile_Precise(t *testing.T) {
	bus := &events.SimpleEventBus{}
	tmpDir := t.TempDir()
	h := history.NewManager(tmpDir + "/history.jsonl")
	counter := &HeuristicTokenCounter{}
	strategy := NewContextStrategy(counter, bus)

	factory := &PipelineFactory{
		History:   h,
		Events:    bus,
		Estimator: strategy,
		Profile:   ProfilePrecise,
	}

	// MaxHistoryTurns = 10. In Precise mode, SlidingWindowPolicy should keep only 5 turns.
	limits := events.Limits{MaxHistoryTurns: 10}
	pipeline := factory.BuildStandardPipeline(limits)

	// Setup history with 10 turns (20 messages)
	ctx := context.Background()
	for i := 1; i <= 10; i++ {
		_ = h.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: fmt.Sprintf("Turn %d", i)}}})
		_ = h.AddContent(ctx, &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "ok"}}})
	}

	req := &ContextRequest{
		History: h.GetContents(),
	}

	err := pipeline.Execute(ctx, req)
	if err != nil {
		t.Fatalf("pipeline execution failed: %v", err)
	}

	// Since we have no importance and no pins, only the sliding window should keep turns.
	// 5 turns = 10 messages.
	if len(req.History) != 10 {
		t.Errorf("expected 10 messages (5 turns) kept in Precise profile, got %d", len(req.History))
	}
}

func TestAgent_WithSessionCostTracker(t *testing.T) {
	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()

	// Mock cost tracker
	tracker := &mockCostTracker{}

	a := New(nil, nil, reg, sm, false, WithSessionCostTracker(tracker))

	if a.tracker != tracker {
		t.Error("WithSessionCostTracker failed to store tracker in Agent")
	}

	if a.engine.costTracker != tracker {
		t.Error("WithSessionCostTracker failed to inject tracker into engine")
	}

	// Verify that tracker is actually called
	metrics := llm.Metrics{PromptTokens: 100, ResponseTokens: 50}
	a.events.Publish(events.UsageMetricsEvent{Metrics: &metrics})

	// Wait a bit for event propagation (SimpleEventBus is synchronous in its current implementation but good practice)
	if tracker.accumulateCalls == 0 {
		t.Error("CostTracker was not notified of metrics")
	}
}

func TestAgent_OperationalMethods(t *testing.T) {
	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()
	tracker := &mockCostTracker{}
	a := New(nil, nil, reg, sm, false, WithSessionCostTracker(tracker))

	t.Run("GetCostTracker", func(t *testing.T) {
		if a.GetCostTracker() != tracker {
			t.Error("GetCostTracker returned wrong tracker")
		}
	})

	t.Run("SetHardBudgetLimit", func(t *testing.T) {
		a.SetHardBudgetLimit(5.0)
		if a.config.HardBudgetLimit != 5.0 {
			t.Errorf("expected budget 5.0, got %f", a.config.HardBudgetLimit)
		}
		if a.engine.HardBudgetLimit != 5.0 {
			t.Errorf("expected engine budget 5.0, got %f", a.engine.HardBudgetLimit)
		}
	})

	t.Run("SetTieredThreshold", func(t *testing.T) {
		a.SetTieredThreshold(100)
		if a.config.Limits.TieredThreshold != 100 {
			t.Errorf("expected threshold 100, got %d", a.config.Limits.TieredThreshold)
		}
	})
}

type mockCostTracker struct {
	accumulateCalls int
}

func (m *mockCostTracker) Accumulate(metrics llm.Metrics)           { m.accumulateCalls++ }
func (m *mockCostTracker) GetTotalCost(ctx context.Context) float64 { return 0 }
func (m *mockCostTracker) GetStats(ctx context.Context) (pricing.UsageStats, float64) {
	return pricing.UsageStats{}, 0
}
func (m *mockCostTracker) CalculateCost(metrics llm.Metrics) float64 { return 0 }
func (m *mockCostTracker) AccumulateAndReturn(metrics llm.Metrics) float64 {
	m.accumulateCalls++
	return 0
}
func (m *mockCostTracker) Warmup() {}

func TestAgent_CoverageEnhancement(t *testing.T) {
	sm := &MockSecurityManager{AllowAll: true}
	reg := registry.New()
	mockClient := &MockLLMClient{}

	t.Run("WithPricing", func(t *testing.T) {
		overrides := map[string]pricing.ModelPricing{
			"model-x": {Miss: 0.01, Comp: 0.02},
		}
		a := New(mockClient, nil, reg, sm, false, WithPricing("model-x", "mode-y", overrides))

		if a.config.Model != "model-x" {
			t.Errorf("expected model-x, got %s", a.config.Model)
		}
		if a.config.Mode != "mode-y" {
			t.Errorf("expected mode-y, got %s", a.config.Mode)
		}
	})

	t.Run("WithSessionCostTracker", func(t *testing.T) {
		tracker := &mockCostTracker{}
		a := New(mockClient, nil, reg, sm, false, WithSessionCostTracker(tracker))

		if a.tracker != tracker {
			t.Error("tracker not correctly assigned")
		}
	})

	t.Run("WithSessionCostTracker_AfterInit", func(t *testing.T) {
		tracker := &mockCostTracker{}
		a := New(mockClient, nil, reg, sm, false)
		// Call it manually on an initialized agent to cover the Reconfigure path
		opt := WithSessionCostTracker(tracker)
		opt(a)

		if a.tracker != tracker {
			t.Error("tracker not correctly assigned")
		}
		if a.engine.costTracker != tracker {
			t.Error("engine tracker not correctly updated")
		}
	})

	t.Run("SetPrunedTurns", func(t *testing.T) {
		a := New(mockClient, nil, reg, sm, false)
		a.SetPrunedTurns(7)

		if a.strategy.prunedTurns != 7 {
			t.Errorf("expected prunedTurns 7, got %d", a.strategy.prunedTurns)
		}
	})

	t.Run("LegacyConfigPaths", func(t *testing.T) {
		a := New(mockClient, nil, reg, sm, false,
			WithMainConfigPath("/tmp/main.yaml"),
			WithPersistentConfigPath("/tmp/pers.json"),
		)

		if a.config.MainConfigPath != "/tmp/main.yaml" {
			t.Errorf("expected /tmp/main.yaml, got %s", a.config.MainConfigPath)
		}
		if a.config.PersistentConfigPath != "/tmp/pers.json" {
			t.Errorf("expected /tmp/pers.json, got %s", a.config.PersistentConfigPath)
		}
	})
}
