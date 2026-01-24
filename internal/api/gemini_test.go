package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage(t *testing.T) {
	// 1. Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		var req Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
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

	// 2. Setup client with mock server URL
	client := NewClient(server.URL, "test-model", "test-key")

	// 3. Execution
	result, err := client.SendMessage("Hello")
	if err != nil {
		t.Fatalf("SendMessage failed: %v", err)
	}

	// 4. Verification
	if result != "World" {
		t.Errorf("expected 'World', got '%s'", result)
	}
}
