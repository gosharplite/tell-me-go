// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/gemini"
)

type toolCapturer struct {
	mu            sync.Mutex
	capturedTools []interface{}
}

func (c *toolCapturer) Set(tools []interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capturedTools = tools
}

func (c *toolCapturer) Get() []interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.capturedTools
}

func TestSearchToolSelection(t *testing.T) {
	tests := []struct {
		name         string
		apiURL       string
		expectSearch bool
	}{
		{
			name:         "Gemini API should use googleSearch",
			apiURL:       "https://generativelanguage.googleapis.com",
			expectSearch: true,
		},
		{
			name:         "Vertex AI should use googleSearch",
			apiURL:       "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models",
			expectSearch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("GOOGLE_API_KEY", "dummy")

			capturer := &toolCapturer{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var req map[string]interface{}
				if err := json.Unmarshal(body, &req); err != nil {
					t.Errorf("failed to unmarshal request body: %v", err)
				}
				if tools, ok := req["tools"].([]interface{}); ok {
					capturer.Set(tools)
				}
				if strings.Contains(r.URL.Path, "streamGenerateContent") {
					// Just return 200 OK to satisfy the request.
					// The SDK might complain about empty stream but we capture tools.
					w.WriteHeader(http.StatusOK)
				} else {
					if _, err := w.Write([]byte(`{"candidates": [{"content": {"parts": [{"text": "ok"}]}}]}`)); err != nil {
						t.Errorf("failed to write response: %v", err)
					}
				}
			}))
			defer server.Close()

			t.Setenv("TELL_ME_MOCK_URL", server.URL)

			bus := events.NewSimpleEventBus(context.Background(), events.WithWorkers(0))
			defer func() { _ = bus.Shutdown(context.Background()) }()
			client, err := gemini.NewClient(tt.apiURL, "model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", true, bus, 5*time.Second)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			// Test SendChat
			_, _, err = client.SendChat(context.Background(), nil, nil, nil)
			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}
			verifyTools(t, capturer.Get(), tt.expectSearch, "SendChat")
		})
	}
}

func verifyTools(t *testing.T, capturedTools []interface{}, expectSearch bool, method string) {
	t.Helper()

	foundSearch := false
	foundRetrieval := false

	for _, tool := range capturedTools {
		toolMap, ok := tool.(map[string]interface{})
		if !ok {
			continue
		}
		if _, ok := toolMap["googleSearch"]; ok {
			foundSearch = true
		}
		if _, ok := toolMap["googleSearchRetrieval"]; ok {
			foundRetrieval = true
		}
	}

	if foundSearch != expectSearch {
		t.Errorf("%s: expected foundSearch=%v, got %v", method, expectSearch, foundSearch)
	}
	if foundRetrieval {
		t.Errorf("%s: expected foundRetrieval=false, got true", method)
	}
}
