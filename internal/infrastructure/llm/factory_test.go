// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/pricing"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestNewClient(t *testing.T) {
	tests := []struct {
		name       string
		apiKey     string
		url        string
		provider   string
		expectAuth reflect.Type
		expectErr  bool
	}{
		{
			name:       "Uses APIKeyAuth when key is present",
			apiKey:     "test-key",
			url:        "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1",
			expectAuth: reflect.TypeOf(&auth.APIKeyAuth{}),
		},
		{
			name:       "Uses BearerAuth for OpenAI",
			apiKey:     "test-key",
			url:        "https://api.openai.com/v1",
			provider:   "openai",
			expectAuth: reflect.TypeOf(&auth.BearerAuth{}),
		},
		{
			name:       "Uses AnthropicAuth for Anthropic",
			apiKey:     "test-key",
			url:        "https://api.anthropic.com/v1",
			provider:   "anthropic",
			expectAuth: reflect.TypeOf(&auth.AnthropicAuth{}),
		},
		{
			name:       "Uses VertexAuth when key is empty",
			apiKey:     "",
			url:        "https://us-central1-aiplatform.googleapis.com/v1/projects/test-project/locations/us-central1",
			expectAuth: reflect.TypeOf(&auth.VertexAuth{}),
		},
		{
			name:      "Returns error if NewGeminiClient fails",
			apiKey:    "test-key",
			url:       "https://generativelanguage.googleapis.com", // Triggers SDK validation error
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pType := tt.provider
			if pType == "" {
				pType = "gemini"
			}
			cfg := &config.Config{
				Providers: map[string]config.LLMProvider{
					"test": {
						Type:   pType,
						APIKey: tt.apiKey,
						URL:    tt.url,
						Model:  "gemini-2.0-flash",
					},
				},
				SelectedProvider: "test",
			}
			pData := pricing.PricingData{
				ThinkingBudgets: map[string]int{
					"default": 1024,
				},
			}
			bus := events.NewSimpleEventBus()

			llmClient, err := NewClient(cfg, pData, bus)
			if tt.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("NewClient failed: %v", err)
			}

			if llmClient == nil {
				t.Fatal("expected non-nil LLMClient")
			}

			// Type-assert the returned llm.LLMClient to *resilientClient
			resilient, ok := llmClient.(*resilientClient)
			if !ok {
				t.Fatalf("expected *resilientClient, got %T", llmClient)
			}

			// Access the inner client field
			v := reflect.ValueOf(resilient.client).Elem()
			f := v.FieldByName("authenticator")
			if f.IsValid() && !f.IsNil() {
				actualAuthType := f.Elem().Type()
				if actualAuthType != tt.expectAuth {
					t.Errorf("expected authenticator type %v, got %v", tt.expectAuth, actualAuthType)
				}
			}
		})
	}
}
