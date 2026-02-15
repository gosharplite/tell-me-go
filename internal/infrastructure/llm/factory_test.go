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
			cfg := &config.Config{
				Providers: map[string]config.LLMProvider{
					"test": {
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

			// Access the inner client field, type-assert it to *Client (the Gemini client)
			// Note: factory.go calls NewGeminiClient which returns *Client
			geminiClient, ok := resilient.client.(*Client)
			if !ok {
				t.Fatalf("expected *Client, got %T", resilient.client)
			}

			// Verify if the authenticator field is correct
			actualAuthType := reflect.TypeOf(geminiClient.authenticator)
			if actualAuthType != tt.expectAuth {
				t.Errorf("expected authenticator type %v, got %v", tt.expectAuth, actualAuthType)
			}
		})
	}
}
