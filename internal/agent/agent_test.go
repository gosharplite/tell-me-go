// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/api"
	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/history"
	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
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
	if a.maxToolTurns != 5 || a.maxHistoryTokens != 1000 || a.maxHistoryTurns != 20 {
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

func TestAgent_EstimatePayloadTokens(t *testing.T) {
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

	tokens := a.estimatePayloadTokens(contents)
	if tokens <= 0 {
		t.Errorf("expected positive token estimate, got %d", tokens)
	}
}

type mockAuth struct {
	authCalls   int
	refreshFunc func() error
	applyFunc   func(req *auth.Request)
}

func (m *mockAuth) GetToken() (string, error) { return "token", nil }
func (m *mockAuth) Invalidate()               { m.authCalls++ }
func (m *mockAuth) Apply(req *auth.Request)   { m.applyFunc(req) }
func (m *mockAuth) RefreshAuth() error        { return m.refreshFunc() }

func TestAgent_Chat_AuthRefresh(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()

	mAuth := &mockAuth{
		applyFunc: func(req *auth.Request) {
			// This is tricky because we can't easily reference 'mAuth' here if it's not defined yet,
			// but we can use a closure.
		},
	}
	// Re-define applyFunc to use mAuth.authCalls
	mAuth.applyFunc = func(req *auth.Request) {
		if mAuth.authCalls == 0 {
			req.Headers["Authorization"] = "Bearer expired"
		} else {
			req.Headers["Authorization"] = "Bearer valid"
		}
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "Bearer expired" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error": {"code": 401, "message": "unauthenticated"}}`))
			return
		}

		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Success"}}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	// Points to our mock server and triggers Vertex logic in initSDK
	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"

	client, err := api.NewClient(apiURL, "test-model", mAuth, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)
	err = a.Chat(context.Background(), "Hello")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if mAuth.authCalls != 1 {
		t.Errorf("Expected 1 auth refresh call, got %d", mAuth.authCalls)
	}
}

func TestAgent_Chat_ToolTimeout(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()

	// Tool that hangs
	registry.Register(&types.ToolDeclaration{
		Name: "slow_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		time.Sleep(200 * time.Millisecond)
		return types.ToolResult{Text: "Too late"}, nil
	})

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var apiResp genai.GenerateContentResponse
		if callCount == 1 {
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "slow_tool"}},
					}}},
				},
			}
		} else {
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Done"}}}},
				},
			}
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)
	a.SetConcurrency(1, 1)                // 1 second timeout
	a.toolTimeout = 50 * time.Millisecond // Overwrite with short timeout

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
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var apiResp genai.GenerateContentResponse
		if callCount == 1 {
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
						{FunctionCall: &genai.FunctionCall{Name: "gen_image"}},
					}}},
				},
			}
		} else {
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "Look at this"}}}},
				},
			}
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)
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

	// Register a dummy tool
	registry.Register(&types.ToolDeclaration{
		Name:       "get_weather",
		Parameters: &types.Schema{Type: "OBJECT"},
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "Sunny"}, nil
	})

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		var apiResp genai.GenerateContentResponse

		if callCount == 1 {
			// First call returns a thought and a function call
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role: "model",
							Parts: []*genai.Part{
								{Text: "I should check the weather.", Thought: true},
								{FunctionCall: &genai.FunctionCall{Name: "get_weather", Args: map[string]interface{}{}}},
							},
						},
					},
				},
			}
		} else {
			// Second call returns final text
			apiResp = genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role:  "model",
							Parts: []*genai.Part{{Text: "It is sunny."}},
						},
					},
				},
			}
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)

	// Execute Chat
	err = a.Chat(context.Background(), "What's the weather?")
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

	registry.Register(&types.ToolDeclaration{
		Name: "infinite_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		return types.ToolResult{Text: "Keep going"}, nil
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{Name: "infinite_tool"}},
				}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)
	a.SetLimits(2, 1000, 20) // Max 2 turns

	err = a.Chat(context.Background(), "Run tool")
	if err != ErrMaxTurnsReached {
		t.Fatalf("Expected ErrMaxTurnsReached, got: %v", err)
	}
}

func TestAgent_Chat_APIError(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "API Failure")
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)
	err = a.Chat(context.Background(), "Hello")
	if err == nil {
		t.Error("Expected error on API failure, got nil")
	}
}

func TestAgent_Chat_ContextCancellation(t *testing.T) {
	tmpDir := t.TempDir()
	hManager := history.NewManager(filepath.Join(tmpDir, "history.json"))
	registry := tools.NewRegistry()

	// Tool that takes some time
	registry.Register(&types.ToolDeclaration{
		Name: "long_tool",
	}, func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
		select {
		case <-time.After(1 * time.Second):
			return types.ToolResult{Text: "Success"}, nil
		case <-ctx.Done():
			return types.ToolResult{}, ctx.Err()
		}
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiResp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{
				{Content: &genai.Content{Role: "model", Parts: []*genai.Part{
					{FunctionCall: &genai.FunctionCall{Name: "long_tool"}},
				}}},
			},
		}
		json.NewEncoder(w).Encode(apiResp)
	}))
	defer server.Close()

	apiURL := server.URL + "/v1/projects/p/locations/l/publishers/google/models/aiplatform.googleapis.com"
	client, err := api.NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", nil, "", false)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	sm := tools.NewSecurityManager()
	a := New(client, hManager, registry, sm)

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the context after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err = a.Chat(ctx, "Run long tool")
	if err == nil {
		t.Error("Expected error due to context cancellation, got nil")
	}
	if err != context.Canceled && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("Expected context.Canceled error, got: %v", err)
	}
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

	if a.maxHistoryTokens != 5000 {
		t.Errorf("expected maxHistoryTokens 5000, got %d", a.maxHistoryTokens)
	}
	if a.maxToolTurns != 15 {
		t.Errorf("expected maxToolTurns 15, got %d", a.maxToolTurns)
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

	if a.maxHistoryTokens != 200000 {
		t.Errorf("expected maxHistoryTokens 200000, got %d", a.maxHistoryTokens)
	}
	if a.maxToolTurns != 25 {
		t.Errorf("expected maxToolTurns 25, got %d", a.maxToolTurns)
	}
	if a.maxHistoryTurns != 30 {
		t.Errorf("expected maxHistoryTurns 30, got %d", a.maxHistoryTurns)
	}
}
