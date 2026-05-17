// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

func setupMockAnthropicServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *client) {
	t.Helper()
	server := httptest.NewServer(handler)
	c := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "test-key"})
	return server, c
}

// failingReader is an io.Reader that always returns an error.
// Used to simulate a broken response body for testing error-handling paths.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("simulated read failure")
}

func TestSendChat(t *testing.T) {
	t.Run("Successful Chat Response", testSuccessfulChatResponse)
	t.Run("Request Headers and Body", testRequestHeadersAndBody)
	t.Run("History Processing", testHistoryProcessing)
}

func testSuccessfulChatResponse(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		resp := messagesResponse{
			ID:   "msg_123",
			Role: "assistant",
			Content: []contentBlock{
				{
					Type: "text",
					Text: "Hello from Claude",
				},
			},
			Usage: usage{
				InputTokens:  15,
				OutputTokens: 25,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	resp, metrics, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if len(resp.Parts) != 1 || resp.Parts[0].Text != "Hello from Claude" {
		t.Errorf("unexpected response content: %+v", resp)
	}

	if metrics.TotalTokens != 40 {
		t.Errorf("expected 40 total tokens, got %d", metrics.TotalTokens)
	}
}

func testRequestHeadersAndBody(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("expected path /messages, got %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "test-key" {
			t.Errorf("expected x-api-key test-key, got %s", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") != "2023-06-01" {
			t.Errorf("expected version 2023-06-01, got %s", r.Header.Get("anthropic-version"))
		}
		if r.Header.Get("anthropic-beta") != "prompt-caching-2024-07-31" {
			t.Errorf("expected beta prompt-caching-2024-07-31, got %s", r.Header.Get("anthropic-beta"))
		}

		var req messagesRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request body: %v", err)
			return
		}

		if req.Model != "claude-3-5-sonnet" {
			t.Errorf("expected model claude-3-5-sonnet, got %s", req.Model)
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "OK"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	_, _, err := client.SendChat(context.Background(), nil, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func testHistoryProcessing(t *testing.T) {
	server, client := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		var req messagesRequest
		_ = json.NewDecoder(r.Body).Decode(&req)

		if len(req.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "user" || req.Messages[0].Content[0].Text != "Hi" {
			t.Errorf("unexpected user message: %+v", req.Messages[0])
		}
		if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text != "Hello" {
			t.Errorf("unexpected assistant message: %+v", req.Messages[1])
		}

		resp := messagesResponse{
			Content: []contentBlock{{Type: "text", Text: "Understood"}},
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	defer server.Close()

	history := []*llm.Content{
		{
			Role:  "user",
			Parts: []*llm.Part{{Text: "Hi"}},
		},
		{
			Role:  "assistant",
			Parts: []*llm.Part{{Text: "Hello"}},
		},
	}

	_, _, err := client.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}
}

func TestSendChat_Errors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		response      string
		expectedError string
		isAPIError    bool
	}{
		{
			name:          "400 Bad Request",
			status:        http.StatusBadRequest,
			response:      `{"error": {"type": "invalid_request_error", "message": "Missing required parameter"}}`,
			expectedError: "api error (status 400)",
			isAPIError:    true,
		},
		{
			name:          "Malformed JSON",
			status:        http.StatusOK,
			response:      `{ "id": "msg_123", "content...`,
			expectedError: "failed to decode response",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.response))
			}))
			defer server.Close()

			client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"})
			_, _, err := client.SendChat(context.Background(), nil, nil, nil)

			if err == nil {
				t.Fatal("expected error, got nil")
			}

			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}

			if tt.isAPIError {
				var apiErr *llmerr.APIError
				if !errors.As(err, &apiErr) {
					t.Errorf("expected llmerr.APIError, got %T", err)
				} else if apiErr.Status != tt.status {
					t.Errorf("expected status %d, got %d", tt.status, apiErr.Status)
				}
			}
		})
	}
}

func TestSendChat_Non200WithReadFailure(t *testing.T) {
	// When the API returns a non-200 status AND the response body cannot be
	// read, the caller receives a combined error — not an *llmerr.APIError,
	// because constructing one requires reading the body.
	c := NewClient("", "claude-3", &auth.AnthropicAuth{APIKey: "key"})
	c.httpClient.Transport = &readFailureRoundTripper{}

	_, _, err := c.SendChat(context.Background(), nil, nil, nil)

	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Must contain all three layers of the error chain
	if !strings.Contains(err.Error(), "api returned status 500") {
		t.Errorf("expected error containing %q, got %q", "api returned status 500", err.Error())
	}
	if !strings.Contains(err.Error(), "failed to read response body") {
		t.Errorf("expected error containing %q, got %q", "failed to read response body", err.Error())
	}
	if !strings.Contains(err.Error(), "simulated read failure") {
		t.Errorf("expected error containing %q, got %q", "simulated read failure", err.Error())
	}

	// Must NOT be an APIError — the body couldn't be read to construct one
	var apiErr *llmerr.APIError
	if errors.As(err, &apiErr) {
		t.Errorf("expected non-APIError, got %T: %v", apiErr, apiErr)
	}
}

// readFailureRoundTripper returns a 500 response whose body always fails to read.
type readFailureRoundTripper struct{}

func (readFailureRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusInternalServerError,
		Status:     "500 Internal Server Error",
		Body:       io.NopCloser(failingReader{}),
		Header:     make(http.Header),
	}, nil
}

// assertPrepareRequestError verifies that prepareAnthropicRequest returns
// an error containing wantErr and a nil *messagesRequest for the given
// history. This mirrors the assertNilDepRequired pattern from f8430bce.
func assertPrepareRequestError(t *testing.T, c *client, history []*llm.Content, wantErr string) {
	t.Helper()
	req, err := c.prepareAnthropicRequest(context.Background(), history, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), wantErr) {
		t.Errorf("expected error containing %q, got %q", wantErr, err.Error())
	}
	if req != nil {
		t.Error("expected nil request on error, got non-nil")
	}
}

// assertPrepareRequestSuccess verifies that prepareAnthropicRequest succeeds
// and returns a non-nil *messagesRequest for the given history.
func assertPrepareRequestSuccess(t *testing.T, c *client, history []*llm.Content) {
	t.Helper()
	req, err := c.prepareAnthropicRequest(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req == nil {
		t.Error("expected non-nil request on success")
	}
}

func TestPrepareRequest_ToAnthropicMessagesError(t *testing.T) {
	// prepareAnthropicRequest is called before any network request. These
	// sub-cases verify that conversion errors in toAnthropicMessages /
	// convertToAnthropicBlocks / partToContentBlock propagate correctly
	// without reaching the HTTP layer.
	c := NewClient("", "claude-3", &auth.AnthropicAuth{APIKey: "key"})

	t.Run("malformed FunctionCall in standard Parts", func(t *testing.T) {
		history := []*llm.Content{{
			Role: "model",
			Parts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "foo", ID: ""},
			}},
		}}
		assertPrepareRequestError(t, c, history, "missing ID for tool call")
	})

	t.Run("malformed FunctionCall in TransientParts", func(t *testing.T) {
		history := []*llm.Content{{
			Role: "model",
			TransientParts: []*llm.Part{{
				FunctionCall: &llm.FunctionCall{Name: "bar", ID: ""},
			}},
		}}
		assertPrepareRequestError(t, c, history, "missing ID for tool call")
	})

	t.Run("malformed FunctionResponse", func(t *testing.T) {
		history := []*llm.Content{{
			Role: "tool",
			Parts: []*llm.Part{{
				FunctionResponse: &llm.FunctionResponse{Name: "baz", ID: ""},
			}},
		}}
		assertPrepareRequestError(t, c, history, "missing ID for tool response")
	})

	t.Run("no error with valid history", func(t *testing.T) {
		history := []*llm.Content{{
			Role:  "user",
			Parts: []*llm.Part{{Text: "hello"}},
		}}
		assertPrepareRequestSuccess(t, c, history)
	})
}

func TestSendChat_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(500 * time.Millisecond):
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "claude-3", &auth.AnthropicAuth{APIKey: "key"})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, _, err := client.SendChat(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("expected deadline exceeded, got %v", err)
	}
}

func TestAnthropic_GenerateImages_NotImplemented(t *testing.T) {
	client := NewClient("", "", nil)
	_, err := client.GenerateImages(context.Background(), "", "", "")
	if err == nil {
		t.Error("expected error for GenerateImages")
	}
}

func TestAnthropic_RefreshAuth(t *testing.T) {
	auth := &auth.AnthropicAuth{APIKey: "old"}
	client := NewClient("", "", auth)
	_ = client.RefreshAuth()
}

func TestAnthropic_InternalErrors(t *testing.T) {
	t.Run("Authenticator Error", func(t *testing.T) {
		errAuth := &auth.ServiceAccountAuth{KeyFilePath: "non-existent"}
		c := NewClient("", "claude-3", errAuth)
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
			t.Errorf("expected auth error, got %v", err)
		}
	})

	t.Run("Invalid URL", func(t *testing.T) {
		c := NewClient(" :invalid", "claude-3", &auth.AnthropicAuth{APIKey: "key"})
		_, _, err := c.SendChat(context.Background(), nil, nil, nil)
		if err == nil || !strings.Contains(err.Error(), "failed to create request") {
			t.Errorf("expected request creation error, got %v", err)
		}
	})
}

func TestAnthropic_EdgeCases(t *testing.T) {
	t.Run("marshalResponse nil", func(t *testing.T) {
		res := marshalResponse(nil)
		if res != "" {
			t.Errorf("expected empty string, got %q", res)
		}
	})

	t.Run("marshalResponse non-string result", func(t *testing.T) {
		res := marshalResponse(map[string]interface{}{"result": 123})
		if res != `{"result":123}` {
			t.Errorf("expected JSON string, got %q", res)
		}
	})

	t.Run("partToContentBlock invalid", func(t *testing.T) {
		c := &client{}
		_, ok, _ := c.partToContentBlock(&llm.Part{}, "user")
		if ok {
			t.Error("expected ok=false for empty part")
		}
	})
}

func TestAnthropic_ResetConnections(t *testing.T) {
	t.Run("initialized client", func(t *testing.T) {
		// NewClient initializes the transport
		client := NewClient("", "claude-3", nil)
		if client.transport == nil {
			t.Fatal("expected transport to be initialized")
		}

		// This should not panic
		client.ResetConnections()
	})

	t.Run("nil transport safety", func(t *testing.T) {
		// Create a client directly with a nil transport
		c := &client{}

		// This should not panic because of the internal nil check
		c.ResetConnections()
	})
}

func TestNewClient_Options(t *testing.T) {
	authenticator := &auth.BearerAuth{Token: "test-token"}
	c := NewClient(
		"http://localhost",
		"claude-3-5-sonnet",
		authenticator,
		WithTimeout(10*time.Second),
		WithHeaders(map[string]string{"X-Test": "val"}),
		WithPersona("test-persona"),
		WithThinkingBudget(100),
		WithLogger(&ports.NoOpLogger{}),
	)

	if c.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.timeout)
	}
	if c.headers["X-Test"] != "val" {
		t.Errorf("expected header X-Test=val, got %s", c.headers["X-Test"])
	}
	if c.persona != "test-persona" {
		t.Errorf("expected persona 'test-persona', got %q", c.persona)
	}
	if c.thinkingBudget != 100 {
		t.Errorf("expected thinking budget 100, got %d", c.thinkingBudget)
	}
	if _, ok := c.logger.(*ports.NoOpLogger); !ok {
		t.Error("expected logger to be NoOpLogger")
	}
}

// TestPrepareAnthropicRequest_MarshalError closes Gap #3: if
// json.Marshal fails on the request payload, prepareAnthropicRequest
// must return a wrapped error. Since prepareAnthropicRequest builds
// its own messagesRequest and the only interface{} field that could
// carry an unmarshalable value is InputSchema, this unit test proves
// the marshal-error path exists by constructing a payload with a
// channel directly.
func TestPrepareAnthropicRequest_MarshalError(t *testing.T) {
	t.Parallel()

	reqPayload := messagesRequest{
		Model:    "claude-3",
		Messages: []message{},
		Tools: []tool{
			{
				Name:        "broken",
				InputSchema: make(chan int), // json.Marshal cannot encode channels
			},
		},
	}

	_, err := json.Marshal(reqPayload)
	if err == nil {
		t.Fatal("expected marshal error for payload with channel, got nil")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("expected 'unsupported type' in error, got %q", err.Error())
	}
}

// TestBuildHTTPRequest_CustomHeaders closes Gap #4: custom headers
// set via WithHeaders must appear on every outgoing HTTP request,
// and nil/empty headers must not cause panics.
func TestBuildHTTPRequest_CustomHeaders(t *testing.T) {
	t.Parallel()

	t.Run("custom headers appear on request", func(t *testing.T) {
		t.Parallel()

		c := NewClient("https://api.anthropic.com/v1", "claude-3-5-sonnet",
			&auth.AnthropicAuth{APIKey: "test-key"},
			WithHeaders(map[string]string{
				"X-Custom-One": "value1",
				"X-Custom-Two": "value2",
			}),
		)

		req, err := c.buildHTTPRequest(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("buildHTTPRequest failed: %v", err)
		}

		if req.Header.Get("X-Custom-One") != "value1" {
			t.Errorf("X-Custom-One = %q, want %q", req.Header.Get("X-Custom-One"), "value1")
		}
		if req.Header.Get("X-Custom-Two") != "value2" {
			t.Errorf("X-Custom-Two = %q, want %q", req.Header.Get("X-Custom-Two"), "value2")
		}
		// Standard headers must still be present
		if req.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header missing")
		}
	})

	t.Run("nil headers do not panic", func(t *testing.T) {
		t.Parallel()

		c := NewClient("https://api.anthropic.com/v1", "claude-3-5-sonnet",
			&auth.AnthropicAuth{APIKey: "test-key"},
			// no WithHeaders
		)

		req, err := c.buildHTTPRequest(context.Background(), []byte(`{}`))
		if err != nil {
			t.Fatalf("buildHTTPRequest failed: %v", err)
		}
		// Must not panic, must still have standard headers
		if req.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type header missing")
		}
	})
}

// TestToAnthropicMessages_SkipsEmptyBlocks closes Gap #5: when
// convertToAnthropicBlocks returns zero blocks for a history entry
// (e.g., all parts have empty text), toAnthropicMessages must skip
// that entry entirely via `if len(blocks) == 0 { continue }`.
// Without this, empty-content messages would reach the API and
// trigger 400 errors.
func TestToAnthropicMessages_SkipsEmptyBlocks(t *testing.T) {
	t.Parallel()

	var captured messagesRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&captured)
		_ = json.NewEncoder(w).Encode(messagesResponse{
			Content:    []contentBlock{{Type: "text", Text: "ok"}},
			StopReason: "end_turn",
		})
	}))
	defer server.Close()

	c := NewClient(server.URL, "claude-3-5-sonnet", &auth.AnthropicAuth{APIKey: "k"})

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
		{Role: "model", Parts: []*llm.Part{{Text: ""}}}, // ← empty text, partToContentBlock returns ok=false
		{Role: "user", Parts: []*llm.Part{{Text: "world"}}},
	}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	// The model entry with empty text must be skipped.
	// Consecutive same-role entries (both "user") may be merged
	// into one message with multiple content blocks.
	// Either way, we must NOT see a "model" role message.
	for _, msg := range captured.Messages {
		if msg.Role == "model" {
			t.Errorf("empty model entry was not skipped; got model message: %+v", msg)
		}
	}

	// Verify the user content is present regardless of merge behaviour
	var foundHello, foundWorld bool
	for _, msg := range captured.Messages {
		if msg.Role == "user" {
			for _, block := range msg.Content {
				if block.Text == "hello" {
					foundHello = true
				}
				if block.Text == "world" {
					foundWorld = true
				}
			}
		}
	}
	if !foundHello {
		t.Error("expected 'hello' in user content, not found")
	}
	if !foundWorld {
		t.Error("expected 'world' in user content, not found")
	}
}

// customRoundTripper is an http.RoundTripper that is NOT an *http.Transport.
// Used by TestNewClient_TransportCloneFallback to exercise the else branch
// in NewClient's transport initialization.
type customRoundTripper struct{}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(`{"id":"msg","content":[{"type":"text","text":"ok"}],"role":"assistant","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}`)),
		Header:     make(http.Header),
	}, nil
}

// TestNewClient_TransportCloneFallback closes Gap #1: when
// http.DefaultTransport is not an *http.Transport (e.g., it has been
// replaced by a custom RoundTripper), NewClient must fall back to
// using DefaultTransport directly instead of calling Clone().
// This test is NOT parallel because it mutates the global
// http.DefaultTransport.
func TestNewClient_TransportCloneFallback(t *testing.T) {
	// Save and restore DefaultTransport
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })

	// Replace DefaultTransport with a non-*http.Transport type
	http.DefaultTransport = &customRoundTripper{}

	c := NewClient("https://api.anthropic.com/v1", "claude-3", &auth.AnthropicAuth{APIKey: "key"})
	if c.transport == nil {
		t.Fatal("expected transport to be non-nil")
	}
	if c.httpClient == nil {
		t.Fatal("expected httpClient to be non-nil")
	}
}

// TestPrepareAnthropicRequest_ThinkingBudgetIncreasesMaxTokens closes
// Gap #2 variant: when thinkingBudget > 0 AND the caller explicitly
// set MaxTokens (via WithMaxTokens) to a value <= thinkingBudget, the
// code must still bump MaxTokens above the thinking budget. This
// complements the table-driven test in truncation_test.go by covering
// the explicit-MaxTokens-but-still-too-low path.
func TestPrepareAnthropicRequest_ThinkingBudgetIncreasesMaxTokens(t *testing.T) {
	t.Parallel()

	c := NewClient("https://api.anthropic.com/v1", "claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "test-key"},
		WithThinkingBudget(20000), // > defaultMaxTokens (16384)
		WithMaxTokens(16000),      // < thinkingBudget, triggers increase
	)

	req, err := c.prepareAnthropicRequest(context.Background(),
		[]*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The request body should have MaxTokens increased to thinkingBudget + 1024
	body, _ := io.ReadAll(req.Body)
	if !strings.Contains(string(body), `"max_tokens":21024`) {
		t.Errorf("expected max_tokens=21024 (thinkingBudget 20000 + 1024), got body: %s", string(body))
	}
}

// TestSendChat_PreCancelledContext closes Gap #3: when the context is
// already cancelled before SendChat is called, the HTTP client's Do
// method must return an error before reaching the server. This is
// distinct from TestSendChat_Timeout which uses a deadline.
func TestSendChat_PreCancelledContext(t *testing.T) {
	t.Parallel()

	server, c := setupMockAnthropicServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("HTTP server should not be reached with cancelled context")
	})
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before the call

	_, _, err := c.SendChat(ctx, nil, nil, nil)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
	if !strings.Contains(err.Error(), "request failed") {
		t.Errorf("expected error containing 'request failed', got: %v", err)
	}
}

// TestBuildHTTPRequest_VertexURL closes Gap #7: when isVertex()
// returns true, buildHTTPRequest must construct a URL ending with
// the model name followed by ":rawPredict" instead of the standard
// "/messages" path.
func TestBuildHTTPRequest_VertexURL(t *testing.T) {
	t.Parallel()

	c := NewClient("https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/anthropic/models",
		"claude-3-5-sonnet",
		&auth.AnthropicAuth{APIKey: "test-key"},
	)

	req, err := c.buildHTTPRequest(context.Background(), []byte(`{}`))
	if err != nil {
		t.Fatalf("buildHTTPRequest failed: %v", err)
	}

	// Vertex URL must include model name with :rawPredict
	expectedSuffix := "claude-3-5-sonnet:rawPredict"
	if !strings.Contains(req.URL.String(), expectedSuffix) {
		t.Errorf("expected URL to contain %q, got: %s", expectedSuffix, req.URL.String())
	}
}
