// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
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
	eventstest.CleanupBus(t, bus)

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

// --- Task H: factory wiring of PROVIDERS.<name>.MAX_TOKENS ---
//
// These tests pin the contract that PROVIDERS.<name>.MAX_TOKENS in
// YAML reaches the per-provider request payload via the appropriate
// option (anthropic.WithMaxTokens, gemini.WithMaxOutputTokens,
// openai.WithMaxTokens). Tests construct a Config pointed at an
// httptest server, call NewClient + SendChat, and inspect the captured
// request body. ResilientClient wraps the base client transparently
// for SendChat so we still see the request hit the mock server.

// runFactorySendChatAndCapture builds a client via NewClient and drives
// one SendChat call against the supplied Config. Callers receive the
// captured request body via their own httptest.Server closure (this
// helper deliberately does not return the body — closures keep the
// per-test capture types flexible).
//
// SendChat may return an error from the mock (e.g., empty response
// body); we only care that the request reached the server.
func runFactorySendChatAndCapture(
	t *testing.T,
	cfg *config.Config,
	pData pricing.PricingData,
) {
	t.Helper()
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	c, err := NewClient(cfg, pData, bus, nil)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient returned nil client")
	}
	_, _, _ = c.SendChat(context.Background(), nil, nil, nil)
}

func TestFactory_PassesMaxTokensToAnthropic(t *testing.T) {
	const want = 12345
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		// Minimal Anthropic-shaped response so the client doesn't error.
		_, _ = w.Write([]byte(`{"id":"x","content":[{"type":"text","text":"ok"}],"role":"assistant","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		SelectedProvider: "claude",
		Providers: map[string]config.LLMProvider{
			"claude": {
				Type:      "anthropic",
				URL:       server.URL,
				Model:     "claude-3-5-sonnet",
				APIKey:    "test-key",
				MaxTokens: want,
			},
		},
	}
	runFactorySendChatAndCapture(t, cfg, pricing.PricingData{})

	if got, _ := captured["max_tokens"].(float64); int(got) != want {
		t.Errorf("anthropic request max_tokens = %v; want %d (full body=%+v)", captured["max_tokens"], want, captured)
	}
}

func TestFactory_PassesMaxTokensToOpenAI(t *testing.T) {
	const want = 12345
	var captured map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		SelectedProvider: "openai",
		Providers: map[string]config.LLMProvider{
			"openai": {
				Type:      "openai",
				URL:       server.URL,
				Model:     "gpt-5", // reasoner → uses max_completion_tokens
				APIKey:    "test-key",
				MaxTokens: want,
			},
		},
	}
	runFactorySendChatAndCapture(t, cfg, pricing.PricingData{})

	if got, _ := captured["max_completion_tokens"].(float64); int(got) != want {
		t.Errorf("openai request max_completion_tokens = %v; want %d (full body=%+v)",
			captured["max_completion_tokens"], want, captured)
	}
}

func TestFactory_PassesMaxTokensToGemini(t *testing.T) {
	const want = 12345
	var capturedConfig map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		// Gemini SDK marshals MaxOutputTokens into a "generationConfig"
		// (or "generation_config") sub-object. Try both keys.
		if g, ok := body["generationConfig"].(map[string]any); ok {
			capturedConfig = g
		} else if g, ok := body["generation_config"].(map[string]any); ok {
			capturedConfig = g
		} else {
			capturedConfig = body
		}
		// Minimal Gemini-shaped response.
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1,"candidatesTokenCount":1,"totalTokenCount":2}}`))
	}))
	defer server.Close()

	// Gemini provider needs Vertex-shaped URL so initSDK picks Vertex backend
	// without external network. Use the test server URL with a vertex-like path.
	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	cfg := &config.Config{
		SelectedProvider: "google",
		Providers: map[string]config.LLMProvider{
			"google": {
				Type:      "gemini",
				URL:       apiURL,
				Model:     "gemini-1.5-flash",
				APIKey:    "test-key", // resolves to APIKeyAuth, no GCP call
				MaxTokens: want,
			},
		},
	}
	runFactorySendChatAndCapture(t, cfg, pricing.PricingData{})

	gotRaw, ok := capturedConfig["maxOutputTokens"]
	if !ok {
		// Some SDK versions may use snake_case
		gotRaw = capturedConfig["max_output_tokens"]
	}
	got, _ := gotRaw.(float64)
	if int(got) != want {
		t.Errorf("gemini request maxOutputTokens = %v; want %d (captured=%+v)", gotRaw, want, capturedConfig)
	}
}

func TestFactory_MaxTokensZero_PreservesProviderDefault(t *testing.T) {
	// Anthropic with MaxTokens=0 should send the package default 16384.
	t.Run("anthropic default", func(t *testing.T) {
		var captured map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"id":"x","content":[{"type":"text","text":"ok"}],"role":"assistant","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
		}))
		defer server.Close()

		cfg := &config.Config{
			SelectedProvider: "claude",
			Providers: map[string]config.LLMProvider{
				"claude": {
					Type:      "anthropic",
					URL:       server.URL,
					Model:     "claude-3-5-sonnet",
					APIKey:    "test-key",
					MaxTokens: 0, // unset → package default
				},
			},
		}
		runFactorySendChatAndCapture(t, cfg, pricing.PricingData{})

		got, _ := captured["max_tokens"].(float64)
		if int(got) != 16384 {
			t.Errorf("anthropic default max_tokens = %v; want 16384", captured["max_tokens"])
		}
	})

	// OpenAI with MaxTokens=0 and ThinkingBudget=0 falls through to default 16384.
	t.Run("openai default both unset", func(t *testing.T) {
		var captured map[string]any
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&captured)
			_, _ = w.Write([]byte(`{"id":"x","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
		}))
		defer server.Close()

		cfg := &config.Config{
			SelectedProvider: "openai",
			Providers: map[string]config.LLMProvider{
				"openai": {
					Type:   "openai",
					URL:    server.URL,
					Model:  "gpt-5", // reasoner
					APIKey: "test-key",
					// MaxTokens and ThinkingBudget both unset
				},
			},
		}
		runFactorySendChatAndCapture(t, cfg, pricing.PricingData{})

		got, _ := captured["max_completion_tokens"].(float64)
		if int(got) != 16384 {
			t.Errorf("openai default max_completion_tokens = %v; want 16384", captured["max_completion_tokens"])
		}
	})
}

// captureLogger returns an slog.Logger that writes warn+ records to
// the returned buffer. Used to assert factory-side soft-warning
// emissions. Wrapped in a ports.Logger adapter so it slots into the
// factory's existing logger parameter.
func captureLogger() (*bytes.Buffer, *slogPortAdapter) {
	buf := &bytes.Buffer{}
	h := slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	return buf, &slogPortAdapter{l: slog.New(h)}
}

type slogPortAdapter struct {
	l *slog.Logger
}

func (a *slogPortAdapter) Debug(msg string, args ...any) { a.l.Debug(msg, args...) }
func (a *slogPortAdapter) Info(msg string, args ...any)  { a.l.Info(msg, args...) }
func (a *slogPortAdapter) Warn(msg string, args ...any)  { a.l.Warn(msg, args...) }
func (a *slogPortAdapter) Error(msg string, args ...any) { a.l.Error(msg, args...) }

func TestFactory_MaxTokensAboveSoftCeiling_EmitsWarning(t *testing.T) {
	buf, logger := captureLogger()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"x","content":[{"type":"text","text":"ok"}],"role":"assistant","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer server.Close()

	cfg := &config.Config{
		SelectedProvider: "claude",
		Providers: map[string]config.LLMProvider{
			"claude": {
				Type:      "anthropic",
				URL:       server.URL,
				Model:     "claude-3-5-sonnet",
				APIKey:    "test-key",
				MaxTokens: 300_000, // above softMaxTokensCeiling
			},
		},
	}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	_, err := NewClient(cfg, pricing.PricingData{}, bus, logger)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	if !strings.Contains(buf.String(), "provider_max_tokens_unusually_high") {
		t.Errorf("expected warn 'provider_max_tokens_unusually_high'; got %q", buf.String())
	}
}
