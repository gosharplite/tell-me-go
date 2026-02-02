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

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

func TestAgent_Setters(t *testing.T) {
	sm := security.NewSecurityManager(nil)
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

	sm := security.NewSecurityManager(nil)
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
	sm := security.NewSecurityManager(nil)

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
	sm := security.NewSecurityManager(nil)

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
	sm := security.NewSecurityManager(nil)

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
	sm := security.NewSecurityManager(nil)

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
	sm := security.NewSecurityManager(nil)

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
	a.SetLimits(2, 1000, 20) // Max 2 turns

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
	sm := security.NewSecurityManager(nil)

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
	sm := security.NewSecurityManager(nil)

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

	sm := security.NewSecurityManager(nil)
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

	sm := security.NewSecurityManager(nil)
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
	sm := security.NewSecurityManager(nil)
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
