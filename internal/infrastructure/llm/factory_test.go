// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestCreateAuthenticator(t *testing.T) {
	t.Run("API key", func(t *testing.T) {
		p := &config.LLMProvider{APIKey: "test-key", Type: "openai"}
		a, err := createAuthenticator(p)
		if err != nil {
			t.Fatalf("failed to create authenticator: %v", err)
		}
		if _, ok := a.(*auth.BearerAuth); !ok {
			t.Errorf("expected *auth.BearerAuth, got %T", a)
		}
	})

	t.Run("GCP Service Account JSON", func(t *testing.T) {
		tmpDir := t.TempDir()
		jsonPath := filepath.Join(tmpDir, "key.json")
		err := os.WriteFile(jsonPath, []byte("{}"), 0600)
		if err != nil {
			t.Fatalf("failed to create dummy json: %v", err)
		}

		p := &config.LLMProvider{APIKey: jsonPath, Type: "google"}
		a, err := createAuthenticator(p)
		if err != nil {
			t.Fatalf("failed to create authenticator: %v", err)
		}
		sa, ok := a.(*auth.ServiceAccountAuth)
		if !ok {
			t.Fatalf("expected *auth.ServiceAccountAuth, got %T", a)
		}
		if sa.KeyFilePath != jsonPath {
			t.Errorf("expected KeyFilePath %s, got %s", jsonPath, sa.KeyFilePath)
		}
	})

	t.Run("Vertex fallback", func(t *testing.T) {
		p := &config.LLMProvider{Type: "google"}
		a, err := createAuthenticator(p)
		if err != nil {
			t.Fatalf("failed to create authenticator: %v", err)
		}
		if _, ok := a.(*auth.VertexAuth); !ok {
			t.Errorf("expected *auth.VertexAuth, got %T", a)
		}
	})
}

func TestCreateAuthenticator_Strategies(t *testing.T) {
	tests := []struct {
		name        string
		provider    config.LLMProvider
		wantErr     bool
		wantAuthNil bool
	}{
		{"OpenAI requires Key", config.LLMProvider{Type: "openai", APIKey: ""}, true, true},
		{"OpenAI valid Key", config.LLMProvider{Type: "openai", APIKey: "secret"}, false, false},
		{"DeepSeek requires Key", config.LLMProvider{Type: "deepseek", APIKey: ""}, true, true},
		{"DeepSeek valid Key", config.LLMProvider{Type: "deepseek", APIKey: "secret"}, false, false},
		{"Anthropic requires Key", config.LLMProvider{Type: "anthropic", APIKey: ""}, true, true},
		{"Anthropic valid Key", config.LLMProvider{Type: "anthropic", APIKey: "secret"}, false, false},
		{"Unknown Provider Fallback with Key", config.LLMProvider{Type: "unknown", APIKey: "secret"}, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			auth, err := createAuthenticator(&tt.provider)

			if (err != nil) != tt.wantErr {
				t.Errorf("createAuthenticator() error = %v, wantErr %v", err, tt.wantErr)
			}

			if (auth == nil) != tt.wantAuthNil {
				t.Errorf("createAuthenticator() auth = %v, wantAuthNil %v", auth, tt.wantAuthNil)
			}
		})
	}
}

func TestCreateAuthenticator_MissingKeysAndFallbacks(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		apiKey   string
		url      string
		wantErr  bool
	}{
		{"openai missing key", "openai", "", "", true},
		{"openai with vertex url", "openai", "", "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gpt-4", false},
		{"deepseek missing key", "deepseek", "", "", true},
		{"deepseek with vertex url", "deepseek", "", "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/deepseek-ai/models/deepseek-r1", false},
		{"anthropic missing key", "anthropic", "", "", true},
		{"unknown provider missing key", "unknown", "", "", true},
		{"google missing key", "google", "", "", false},                     // Resolves to VertexAuth
		{"unknown provider with key", "unknown", "explicit-key", "", false}, // Resolves to APIKeyAuth
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &config.LLMProvider{
				Type:   tt.provider,
				APIKey: tt.apiKey,
				URL:    tt.url,
			}
			a, err := createAuthenticator(p)
			if (err != nil) != tt.wantErr {
				t.Errorf("createAuthenticator() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr {
				if tt.url != "" && strings.Contains(tt.url, "aiplatform.googleapis.com") {
					if _, ok := a.(*auth.VertexAuth); !ok {
						t.Errorf("expected *auth.VertexAuth for vertex url, got %T", a)
					}
				}

				// If it's the unknown provider with a key, verify it uses APIKeyAuth
				if tt.provider == "unknown" && tt.apiKey != "" {
					if _, ok := a.(*auth.APIKeyAuth); !ok {
						t.Errorf("expected *auth.APIKeyAuth for unknown provider with key, got %T", a)
					}
				}
			}
		})
	}
}

func TestNewClient_FallbackToGemini(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "dummy")
	cfg := &config.Config{
		Providers: map[string]config.LLMProvider{
			"default": {
				Type:   "some-unknown-type",
				APIKey: "dummy-key", // Satisfies createAuthenticator fallback
			},
		},
		SelectedProvider: "default",
	}

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)

	pData := pricing.PricingData{}

	// This should hit the default: case in the switch statement
	client, err := NewClient(cfg, pData, bus, nil)
	if err != nil {
		t.Fatalf("NewClient() unexpected error: %v", err)
	}
	if client == nil {
		t.Fatal("expected client, got nil")
	}
}

func TestResolveTimeout(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *config.Config
		expected int
	}{
		{
			name:     "Default to 60s when zero",
			cfg:      &config.Config{HTTPTimeoutSeconds: 0},
			expected: 60,
		},
		{
			name:     "Custom timeout used when non-zero",
			cfg:      &config.Config{HTTPTimeoutSeconds: 120},
			expected: 120,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTimeout(tt.cfg)
			if int(got.Seconds()) != tt.expected {
				t.Errorf("resolveTimeout() = %v, want %ds", got, tt.expected)
			}
		})
	}
}
