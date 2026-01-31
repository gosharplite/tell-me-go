// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/auth"
	"github.com/gosharplite/tell-me-go/internal/types"
)

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
			os.Setenv("GOOGLE_API_KEY", "dummy")
			defer os.Unsetenv("GOOGLE_API_KEY")

			var capturedTools []interface{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var req map[string]interface{}
				json.Unmarshal(body, &req)
				if tools, ok := req["tools"].([]interface{}); ok {
					capturedTools = tools
				}
				if strings.Contains(r.URL.Path, "streamGenerateContent") {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, `[{"candidates": [{"content": {"parts": [{"text": "ok"}]}}]}]`)
				} else {
					w.Write([]byte(`{"candidates": [{"content": {"parts": [{"text": "ok"}]}}]}`))
				}
			}))
			defer server.Close()

			os.Setenv("TELL_ME_MOCK_URL", server.URL)
			defer os.Unsetenv("TELL_ME_MOCK_URL")

			client, err := NewClient(tt.apiURL, "model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", true)
			if err != nil {
				t.Fatalf("failed to create client: %v", err)
			}

			// Test SendChat
			_, _, err = client.SendChat(context.Background(), nil, nil, nil)
			if err != nil {
				t.Fatalf("SendChat failed: %v", err)
			}
			verifyTools(t, capturedTools, tt.expectSearch, "SendChat")

			// Reset capturedTools
			capturedTools = nil

			// Test StreamChat
			capturedTools = nil
			_, _ = client.StreamChat(context.Background(), nil, nil, nil, func(c *types.Content) {})
			verifyTools(t, capturedTools, tt.expectSearch, "StreamChat")
		})
	}
}

func verifyTools(t *testing.T, capturedTools []interface{}, expectSearch bool, method string) {
	foundSearch := false
	foundRetrieval := false

	for _, tool := range capturedTools {
		toolMap := tool.(map[string]interface{})
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
