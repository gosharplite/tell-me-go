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

	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestAgent_Setters(t *testing.T) {
	sm := tools.NewSecurityManager()
	registry := tools.NewRegistry()
	a := New(nil, nil, registry, sm)
	a.SetUIOptions(false, false)
	if a.showThoughts || a.showTools {
		t.Error("SetUIOptions failed")
	}

	a.SetLimits(5, 1000, 20)
	maxTokens, maxTurns, maxHistTurns := a.contextManager.GetLimits()
	if maxTurns != 5 || maxTokens != 1000 || maxHistTurns != 20 {
		t.Error("SetLimits failed")
	}

	a.SetConcurrency(10, 60)
	if a.maxConcurrentTools != 10 || a.toolTimeout != 60*time.Second {
		t.Error("SetConcurrency failed")
	}

	a.SetLogFile("test.log")
	if a.logFile != "test.log" {
		t.Error("SetLogFile failed")
	}
}

func TestAgent_EstimateTokens(t *testing.T) {
	registry := tools.NewRegistry()
	registry.Register(&types.ToolDeclaration{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  &types.Schema{Type: "OBJECT"},
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "ok"}, nil
	})

	sm := tools.NewSecurityManager()
	a := New(nil, nil, registry, sm)

	contents := []*types.Content{
		{
			Role: "user",
			Parts: []*types.Part{
				{Text: "Hello world"},
				{FunctionCall: &types.FunctionCall{Name: "test_tool", Args: map[string]interface{}{"a": 1}}},
				{FunctionResponse: &types.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"res": "ok"}}},
			},
		},
	}

	tokens := a.contextManager.EstimateTokens(contents)
	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}
}

func TestAgent_Chat_AuthRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	authCalls := 0
	mockClient := &MockLLMClient{
		RefreshAuthFn: func() error {
			authCalls++
			return nil
		},
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			if authCalls == 0 {
				return nil, fmt.Errorf("401 Unauthorized")
			}
			callback(&types.Content{Role: "model", Parts: []*types.Part{{Text: "Success"}}})
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)
	err := a.Chat(context.Background(), "Hello")
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
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	// Tool that hangs
	registry.Register(&types.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		time.Sleep(200 * time.Millisecond)
		return types.ToolResult{Text: "Too late"}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&types.Content{Role: "model", Parts: []*types.Part{
					{FunctionCall: &types.FunctionCall{Name: "slow_tool"}},
				}})
			} else {
				callback(&types.Content{Role: "model", Parts: []*types.Part{{Text: "Done"}}})
			}
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)
	a.toolTimeout = 50 * time.Millisecond // Short timeout

	_ = a.Chat(context.Background(), "Run slow tool")

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
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	registry.Register(&types.ToolDeclaration{
		Name: "gen_image",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8/5+hHgAHggJ/PchI7wAAAABJRU5ErkJggg==")
		return types.ToolResult{
			Text: "Image generated",
			BinaryData: []types.BinaryData{
				{MIMEType: "image/png", Data: data},
			},
		}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&types.Content{Role: "model", Parts: []*types.Part{
					{FunctionCall: &types.FunctionCall{Name: "gen_image"}},
				}})
			} else {
				callback(&types.Content{Role: "model", Parts: []*types.Part{{Text: "Look at this"}}})
			}
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)
	_ = a.Chat(context.Background(), "Generate an image")

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
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	// Register a dummy tool
	registry.Register(&types.ToolDeclaration{
		Name:       "get_weather",
		Parameters: &types.Schema{Type: "OBJECT"},
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "Sunny"}, nil
	})

	callCount := 0
	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callCount++
			if callCount == 1 {
				callback(&types.Content{
					Role: "model",
					Parts: []*types.Part{
						{Text: "I should check the weather.", Thought: true},
						{FunctionCall: &types.FunctionCall{Name: "get_weather", Args: map[string]interface{}{}}},
					},
				})
			} else {
				callback(&types.Content{
					Role:  "model",
					Parts: []*types.Part{{Text: "It is sunny."}},
				})
			}
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)

	// Execute Chat
	err := a.Chat(context.Background(), "What's the weather?")
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
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	registry.Register(&types.ToolDeclaration{
		Name: "infinite_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "Keep going"}, nil
	})

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callback(&types.Content{Role: "model", Parts: []*types.Part{
				{FunctionCall: &types.FunctionCall{Name: "infinite_tool"}},
			}})
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)
	a.SetLimits(2, 1000, 20) // Max 2 turns

	err := a.Chat(context.Background(), "Run tool")
	if !errors.Is(err, ErrMaxTurnsReached) {
		t.Fatalf("Expected ErrMaxTurnsReached, got: %v", err)
	}
}

func TestAgent_Chat_APIError(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			return nil, fmt.Errorf("API Failure")
		},
	}

	a := New(mockClient, hManager, registry, sm)
	err := a.Chat(context.Background(), "Hello")
	if err == nil {
		t.Error("Expected error on API failure, got nil")
	}
}

func TestAgent_Chat_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()
	sm := tools.NewSecurityManager()

	// Tool that takes some time
	running := make(chan struct{})
	registry.Register(&types.ToolDeclaration{
		Name: "long_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		close(running)
		select {
		case <-time.After(1 * time.Second):
			return types.ToolResult{Text: "Success"}, nil
		case <-ctx.Done():
			return types.ToolResult{}, ctx.Err()
		}
	})

	mockClient := &MockLLMClient{
		StreamChatFn: func(ctx context.Context, history []*types.Content, tools []*types.ToolDeclaration, resolver types.AssetResolver, callback func(*types.Content)) (*types.Metrics, error) {
			callback(&types.Content{Role: "model", Parts: []*types.Part{
				{FunctionCall: &types.FunctionCall{Name: "long_tool"}},
			}})
			return &types.Metrics{}, nil
		},
	}

	a := New(mockClient, hManager, registry, sm)

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

		err := a.Chat(ctx, "Run long tool")
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

	sm := tools.NewSecurityManager()
	registry := tools.NewRegistry()
	a := New(nil, nil, registry, sm)
	a.SetLimits(10, 1000, 20)

	// Set the config path
	a.SetPersistentConfigPath(configPath)

	// Create config file with string values
	configContent := `{"MAX_HISTORY_TOKENS": "5000", "MAX_TOOL_TURNS": "15"}`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	a.refreshLimits()

	maxTokens, maxTurns, _ := a.contextManager.GetLimits()
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

	sm := tools.NewSecurityManager()
	registry := tools.NewRegistry()
	a := New(nil, nil, registry, sm)
	a.SetLimits(10, 1000, 20)

	// Set the main config path
	a.SetMainConfigPath(yamlPath)

	// Create YAML config file
	yamlContent := "MAX_HISTORY_TOKENS: 200000\nMAX_TURNS: 25\nMAX_HISTORY_TURNS: 30"
	if err := os.WriteFile(yamlPath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write yaml config file: %v", err)
	}

	a.refreshLimits()

	maxTokens, maxTurns, maxHistTurns := a.contextManager.GetLimits()
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
