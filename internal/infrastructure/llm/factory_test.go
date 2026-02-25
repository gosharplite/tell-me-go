// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
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
		{"Local uses NoOp", config.LLMProvider{Type: "local"}, false, false},
		{"Ollama uses NoOp", config.LLMProvider{Type: "ollama"}, false, false},
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
