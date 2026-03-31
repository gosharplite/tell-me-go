// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"reflect"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name           string
		providerType   string
		apiKey         string
		url            string
		mockURL        string
		timeoutSeconds int
		expectAuthType reflect.Type
		expectErr      bool
	}{
		{
			name:           "Success with Gemini API Key",
			providerType:   "gemini",
			apiKey:         "test-key",
			url:            "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l",
			expectAuthType: reflect.TypeOf(&auth.APIKeyAuth{}),
		},
		{
			name:           "Success with OpenAI Bearer",
			providerType:   "openai",
			apiKey:         "test-key",
			url:            "https://api.openai.com/v1",
			expectAuthType: reflect.TypeOf(&auth.BearerAuth{}),
		},
		{
			name:           "Success with Anthropic Auth",
			providerType:   "anthropic",
			apiKey:         "test-key",
			url:            "https://api.anthropic.com/v1",
			expectAuthType: reflect.TypeOf(&auth.AnthropicAuth{}),
		},
		{
			name:           "Success with Vertex (No Key)",
			providerType:   "gemini",
			apiKey:         "",
			url:            "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l",
			expectAuthType: reflect.TypeOf(&auth.VertexAuth{}),
		},
		{
			name:         "Failure on missing API Key for OpenAI",
			providerType: "openai",
			apiKey:       "",
			expectErr:    true,
		},
		{
			name:           "Success with custom timeout",
			providerType:   "gemini",
			apiKey:         "test-key",
			url:            "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l",
			timeoutSeconds: 30,
			expectAuthType: reflect.TypeOf(&auth.APIKeyAuth{}),
		},
		{
			name:           "Success with mock URL",
			providerType:   "gemini",
			apiKey:         "test-key",
			url:            "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l",
			mockURL:        "http://localhost:8080",
			expectAuthType: reflect.TypeOf(&auth.APIKeyAuth{}),
		},
		{
			name:         "Returns error if NewGeminiClient fails (SDK validation)",
			providerType: "gemini",
			apiKey:       "test-key",
			url:          "https://generativelanguage.googleapis.com",
			expectErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.mockURL != "" {
				t.Setenv("TELL_ME_MOCK_URL", tt.mockURL)
			}

			cfg := &config.Config{
				Providers: map[string]config.LLMProvider{
					"test": {
						Type:   tt.providerType,
						APIKey: tt.apiKey,
						URL:    tt.url,
						Model:  "model-1",
					},
				},
				SelectedProvider:   "test",
				HTTPTimeoutSeconds: tt.timeoutSeconds,
			}
			pData := pricing.PricingData{}
			bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			inframock.CleanupBus(t, bus)

			client, err := NewClient(cfg, pData, bus, nil)

			if tt.expectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, client)

			resilient, ok := client.(*resilientClient)
			require.True(t, ok)

			// Access the inner client's authenticator using reflection
			v := reflect.ValueOf(resilient.client).Elem()
			f := v.FieldByName("authenticator")
			if f.IsValid() && !f.IsNil() {
				assert.Equal(t, tt.expectAuthType, f.Elem().Type())
			}
		})
	}
}

type mockHttpStatusErr struct {
	code int
}

func (m mockHttpStatusErr) StatusCode() int { return m.code }
func (m mockHttpStatusErr) Error() string   { return fmt.Sprintf("HTTP %d", m.code) }

func TestResilientClient_WrapError(t *testing.T) {
	client := &resilientClient{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{"Nil error", nil, nil},
		{"Already Auth", llm.ErrAuth, llm.ErrAuth},
		{"Already Transient", llm.ErrTransient, llm.ErrTransient},
		{"Already Terminal", llm.ErrTerminal, llm.ErrTerminal},
		{"Already RateLimit", llm.ErrRateLimit, llm.ErrRateLimit},

		{"gRPC Unauthenticated", status.Error(codes.Unauthenticated, "fail"), llm.ErrAuth},
		{"gRPC Unavailable", status.Error(codes.Unavailable, "fail"), llm.ErrTransient},
		{"gRPC PermissionDenied", status.Error(codes.PermissionDenied, "fail"), llm.ErrTerminal},

		{"HTTP 401", mockHttpStatusErr{401}, llm.ErrAuth},
		{"HTTP 429", mockHttpStatusErr{429}, llm.ErrRateLimit},
		{"HTTP 500", mockHttpStatusErr{500}, llm.ErrTransient},
		{"HTTP 404", mockHttpStatusErr{404}, llm.ErrTerminal},

		// Verify delegation to llmerr.Classify string matching for one case
		{"String match Rate Limit", errors.New("RATE_LIMIT_EXCEEDED"), llm.ErrRateLimit},

		{"Generic fallback", errors.New("unknown error"), llm.ErrTerminal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.wrapError(tt.err)
			if tt.expected == nil {
				assert.NoError(t, got)
				return
			}
			assert.ErrorIs(t, got, tt.expected)
		})
	}
}

type mockLLMClient struct {
	sendChatFn       func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	refreshAuthFn    func() error
	generateImagesFn func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error)
	authRefreshed    int
	resetCalled      bool // New field
}

func (m *mockLLMClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	if m.sendChatFn != nil {
		return m.sendChatFn(ctx, history, tools, resolver)
	}
	return nil, nil, nil
}

func (m *mockLLMClient) RefreshAuth() error {
	m.authRefreshed++
	if m.refreshAuthFn != nil {
		return m.refreshAuthFn()
	}
	return nil
}

func (m *mockLLMClient) GenerateImages(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
	if m.generateImagesFn != nil {
		return m.generateImagesFn(ctx, model, prompt, mimeType)
	}
	return nil, nil
}

func (m *mockLLMClient) ResetConnections() {
	m.resetCalled = true
}

func TestResilientClient_Generate_RetryAuth(t *testing.T) {
	var mock *mockLLMClient
	mock = &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			if mock.authRefreshed == 0 {
				return nil, nil, status.Error(codes.Unauthenticated, "expired")
			}
			return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "success"}}}, &llm.Metrics{}, nil
		},
	}

	client := NewResilientClient(mock)
	content, _, err := client.Generate(context.Background(), nil, nil, nil)

	if err != nil {
		t.Fatalf("Expected success after retry, got error: %v", err)
	}
	if content.Parts[0].Text != "success" {
		t.Errorf("Expected 'success', got %v", content.Parts[0].Text)
	}
	if mock.authRefreshed != 1 {
		t.Errorf("Expected 1 auth refresh, got %d", mock.authRefreshed)
	}
}

func TestResilientClient_Generate_AuthRefreshFail(t *testing.T) {
	mock := &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, status.Error(codes.Unauthenticated, "expired")
		},
		refreshAuthFn: func() error {
			return errors.New("refresh failed")
		},
	}

	client := NewResilientClient(mock)
	_, _, err := client.Generate(context.Background(), nil, nil, nil)

	if !errors.Is(err, llm.ErrAuth) {
		t.Errorf("Expected ErrAuth, got %v", err)
	}
	if mock.authRefreshed != 1 {
		t.Errorf("Expected 1 auth refresh attempt, got %d", mock.authRefreshed)
	}
}

func TestResilientClient_ErrorDelegation(t *testing.T) {
	mockErr := errors.New("mock execution error")
	mock := &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, mockErr
		},
		refreshAuthFn: func() error {
			return mockErr
		},
		generateImagesFn: func(ctx context.Context, model, prompt string, mimeType string) ([][]byte, error) {
			return nil, mockErr
		},
	}

	client := NewResilientClient(mock)

	t.Run("SendChat Error Delegation", func(t *testing.T) {
		_, _, err := client.SendChat(context.Background(), nil, nil, nil)
		if !errors.Is(err, mockErr) {
			t.Errorf("Expected %v, got %v", mockErr, err)
		}
	})

	t.Run("RefreshAuth Error Delegation", func(t *testing.T) {
		err := client.RefreshAuth()
		if !errors.Is(err, mockErr) {
			t.Errorf("Expected %v, got %v", mockErr, err)
		}
	})

	t.Run("GenerateImages Error Delegation", func(t *testing.T) {
		_, err := client.GenerateImages(context.Background(), "model", "prompt", "image/png")
		if !errors.Is(err, mockErr) {
			t.Errorf("Expected %v, got %v", mockErr, err)
		}
	})
}

func TestResilientClient_Generate_ResetConnections(t *testing.T) {
	t.Run("Resets on Rate Limit", func(t *testing.T) {
		mock := &mockLLMClient{
			sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return nil, nil, llm.ErrRateLimit
			},
		}
		client := NewResilientClient(mock)
		_, _, _ = client.Generate(context.Background(), nil, nil, nil)
		if !mock.resetCalled {
			t.Error("expected ResetConnections to be called on rate limit")
		}
	})

	t.Run("Resets on Transient Error", func(t *testing.T) {
		mock := &mockLLMClient{
			sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
				return nil, nil, llm.ErrTransient
			},
		}
		client := NewResilientClient(mock)
		_, _, _ = client.Generate(context.Background(), nil, nil, nil)
		if !mock.resetCalled {
			t.Error("expected ResetConnections to be called on transient error")
		}
	})
}

func TestResilientClient_Generate_ResetsOnFinalAttempt(t *testing.T) {
	var m *mockLLMClient
	m = &mockLLMClient{
		sendChatFn: func(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			if m.authRefreshed == 0 {
				return nil, nil, llm.ErrAuth
			}
			return nil, nil, errors.New("persistent failure")
		},
	}
	client := NewResilientClient(m)

	_, _, _ = client.Generate(context.Background(), nil, nil, nil)

	if !m.resetCalled {
		t.Error("expected ResetConnections to be called on the final attempt (attempt 1) after auth retry")
	}
}
