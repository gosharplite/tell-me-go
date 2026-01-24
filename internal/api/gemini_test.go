// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/auth"
)

func TestSendChat(t *testing.T) {
	// 1. Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify Headers (Simulating Vertex Auth)
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
		}

		// Verify request
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}
		if req.Contents[0].Role != "user" {
			t.Errorf("expected role 'user', got '%s'", req.Contents[0].Role)
		}
		if req.Contents[0].Parts[0].Text != "Hello" {
			t.Errorf("expected prompt 'Hello', got '%s'", req.Contents[0].Parts[0].Text)
		}

		// Send mock response
		resp := Response{
			Candidates: []Candidate{
				{
					Content: Content{
						Parts: []Part{{Text: "World"}},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// 2. Setup client with mock server URL and mock authenticator
	authenticator := &auth.VertexAuth{Token: "test-token"}
	client := NewClient(server.URL, "test-model", authenticator)

	// 3. Execution
	history := []Content{
		{Role: "user", Parts: []Part{{Text: "Hello"}}},
	}
	result, err := client.SendChat(history)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	// 4. Verification
	if result != "World" {
		t.Errorf("expected 'World', got '%s'", result)
	}
}
