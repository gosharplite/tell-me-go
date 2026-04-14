// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"google.golang.org/genai"
)

func validateAuthOnly(t *testing.T, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
	}
}

func validateSystemInstructionReq(t *testing.T, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
	}
	var req struct {
		SystemInstruction struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"systemInstruction"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.Errorf("failed to decode request: %v", err)
	}
	expected := "Be helpful and concise"
	if len(req.SystemInstruction.Parts) == 0 || req.SystemInstruction.Parts[0].Text != expected {
		t.Errorf("expected system instruction %q, got %v", expected, req.SystemInstruction.Parts)
	}
}

type sendChatTestCase struct {
	name                   string
	mockResponse           genai.GenerateContentResponse
	expectedText           string
	expectedError          string
	systemInstruction      string
	thinkingBudget         int
	thinkingBudgetSeverity string
	validateReq            func(t *testing.T, r *http.Request)
}

func TestSendChat_Scenarios(t *testing.T) {
	tests := []sendChatTestCase{
		{
			name: "Success",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						Content: &genai.Content{
							Role:  "model",
							Parts: []*genai.Part{{Text: "World"}},
						},
					},
				},
			},
			expectedText: "World",
			validateReq:  validateAuthOnly,
		},
		{
			name: "SafetyBlock",
			mockResponse: genai.GenerateContentResponse{
				PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
					BlockReason: "SAFETY",
				},
			},
			expectedError: "blocked by safety filters",
			validateReq:   validateAuthOnly,
		},
		{
			name: "FinishReasonSafety",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{
					{
						FinishReason: genai.FinishReasonSafety,
					},
				},
			},
			expectedError: "Finish Reason: SAFETY",
			validateReq:   validateAuthOnly,
		},
		{
			name: "SystemInstruction",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
			},
			systemInstruction: "Be helpful and concise",
			expectedText:      "OK",
			validateReq:       validateSystemInstructionReq,
		},
		{
			name: "ThinkingBudget",
			mockResponse: genai.GenerateContentResponse{
				Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
			},
			thinkingBudget:         1000,
			thinkingBudgetSeverity: "LOW",
			expectedText:           "OK",
			validateReq:            validateAuthOnly,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runSendChatTest(t, tt)
		})
	}
}

func runSendChatTest(t *testing.T, tt sendChatTestCase) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tt.validateReq != nil {
			tt.validateReq(t, r)
		}

		if err := json.NewEncoder(w).Encode(tt.mockResponse); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	client, err := NewClient(
		apiURL,
		"test-model",
		authenticator,
		WithThinking(tt.thinkingBudget, tt.thinkingBudgetSeverity, 0),
		WithSystemInstruction(tt.systemInstruction),
		WithEventBus(bus),
		WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
	}
	content, _, err := client.SendChat(context.Background(), history, nil, nil)

	if tt.expectedError != "" {
		if err == nil {
			t.Fatalf("expected error containing %q, got nil", tt.expectedError)
		}
		if !strings.Contains(err.Error(), tt.expectedError) {
			t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
		}
		return
	}

	if err != nil {
		t.Fatalf("SendChat failed: %v", err)
	}

	if content.Parts[0].Text != tt.expectedText {
		t.Errorf("expected text %q, got %q", tt.expectedText, content.Parts[0].Text)
	}
}

func TestRefreshAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := genai.GenerateContentResponse{
			Candidates: []*genai.Candidate{{Content: &genai.Content{Parts: []*genai.Part{{Text: "OK"}}}}},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.VertexAuth{Token: "test-token"}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	client, _ := NewClient(apiURL, "test-model", authenticator, WithEventBus(bus), WithTimeout(5*time.Second))

	err := client.RefreshAuth()
	if err != nil {
		t.Fatalf("RefreshAuth failed: %v", err)
	}
}

type generateImagesTestCase struct {
	name          string
	mockResponse  genai.GenerateImagesResponse
	mockError     error
	expectedCount int
	expectedError string
}

func TestGenerateImages(t *testing.T) {
	tests := []generateImagesTestCase{
		{
			name: "Success",
			mockResponse: genai.GenerateImagesResponse{
				GeneratedImages: []*genai.GeneratedImage{
					{
						Image: &genai.Image{
							ImageBytes: []byte("image1"),
						},
					},
					{
						Image: &genai.Image{
							ImageBytes: []byte("image2"),
						},
					},
				},
			},
			expectedCount: 2,
		},
		{
			name:          "NoImagesGenerated",
			mockResponse:  genai.GenerateImagesResponse{},
			expectedError: "no images generated",
		},
		{
			name:          "ServerError",
			mockError:     fmt.Errorf("internal server error"),
			expectedError: "internal server error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runGenerateImagesTest(t, tt)
		})
	}
}

func runGenerateImagesTest(t *testing.T, tt generateImagesTestCase) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleGenerateImagesMock(t, w, r, tt)
	}))
	t.Cleanup(server.Close)

	t.Setenv("TELL_ME_MOCK_URL", server.URL)
	t.Setenv("GOOGLE_API_KEY", "dummy")
	apiURL := "http://localhost/v1" // Trigger GeminiAPI backend with v1
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	client, err := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, WithEventBus(bus), WithTimeout(5*time.Second))
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	images, err := client.GenerateImages(context.Background(), "imagen-3.0", "a cat", "image/png")
	assertGenerateImagesResults(t, tt, images, err)
}

func handleGenerateImagesMock(t *testing.T, w http.ResponseWriter, r *http.Request, tt generateImagesTestCase) {
	if tt.mockError != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, tt.mockError.Error())
		return
	}

	if tt.name == "Success" {
		// Return multiple formats to be safe across different SDK/API versions
		resp := map[string]interface{}{
			"generatedImages": []map[string]interface{}{
				{"image": map[string]interface{}{"imageBytes": "aW1hZ2Ux"}},
				{"image": map[string]interface{}{"imageBytes": "aW1hZ2Uy"}},
			},
			"predictions": []map[string]interface{}{
				{"bytesBase64Encoded": "aW1hZ2Ux", "mimeType": "image/png"},
				{"bytesBase64Encoded": "aW1hZ2Uy", "mimeType": "image/png"},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode mock response: %v", err)
		}
		return
	}

	if err := json.NewEncoder(w).Encode(tt.mockResponse); err != nil {
		t.Errorf("failed to encode response: %v", err)
	}
}

func assertGenerateImagesResults(t *testing.T, tt generateImagesTestCase, images [][]byte, err error) {
	if tt.expectedError != "" {
		if err == nil || !strings.Contains(err.Error(), tt.expectedError) {
			t.Errorf("expected error containing %q, got %v", tt.expectedError, err)
		}
		return
	}

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(images) != tt.expectedCount {
		t.Errorf("expected %d images, got %d", tt.expectedCount, len(images))
	}
}

func TestToSDKSchema(t *testing.T) {

	s := &tools.Schema{
		Type:        "object",
		Description: "test schema",
		Required:    []string{"prop1"},
		Properties: map[string]*tools.Schema{
			"prop1": {
				Type:        "string",
				Description: "property 1",
			},
			"prop2": {
				Type: "array",
				Items: &tools.Schema{
					Type: "integer",
				},
			},
		},
	}

	sdkSchema := toSDKSchema(s)

	if string(sdkSchema.Type) != "object" {
		t.Errorf("expected type %q, got %q", "object", sdkSchema.Type)
	}
	if sdkSchema.Description != "test schema" {
		t.Errorf("expected description %q, got %q", "test schema", sdkSchema.Description)
	}
	if len(sdkSchema.Required) != 1 || sdkSchema.Required[0] != "prop1" {
		t.Errorf("expected required %v, got %v", []string{"prop1"}, sdkSchema.Required)
	}
	if string(sdkSchema.Properties["prop1"].Type) != "string" {
		t.Errorf("expected prop1 type %q, got %q", "string", sdkSchema.Properties["prop1"].Type)
	}
	if string(sdkSchema.Properties["prop2"].Type) != "array" {
		t.Errorf("expected prop2 type %q, got %q", "array", sdkSchema.Properties["prop2"].Type)
	}
	if string(sdkSchema.Properties["prop2"].Items.Type) != "integer" {
		t.Errorf("expected prop2 items type %q, got %q", "integer", sdkSchema.Properties["prop2"].Items.Type)
	}

	// Test nil schema
	if toSDKSchema(nil) != nil {
		t.Error("expected nil for nil input")
	}
}

func TestClassifyError(t *testing.T) {

	client := &Client{}

	tests := []struct {
		name     string
		err      error
		expected error
	}{
		{
			name:     "NilError",
			err:      nil,
			expected: nil,
		},
		{
			name:     "GenericError",
			err:      fmt.Errorf("some error"),
			expected: fmt.Errorf("%w: some error", llm.ErrTerminal),
		},
		{
			name:     "RateLimitError",
			err:      fmt.Errorf("Error 429, Message: Resource exhausted."),
			expected: fmt.Errorf("%w: Error 429, Message: Resource exhausted.", llm.ErrRateLimit),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.classifyError(tt.err)
			if (got == nil) != (tt.expected == nil) {
				t.Fatalf("expected error %v, got %v", tt.expected, got)
			}
			if got != nil && !errors.Is(got, tt.expected) && got.Error() != tt.expected.Error() {
				t.Errorf("expected error message %q, got %q", tt.expected.Error(), got.Error())
			}
		})
	}
}

func TestToSDKTool(t *testing.T) {

	tests := []struct {
		name     string
		input    []*tools.ToolDeclaration
		validate func(t *testing.T, result []*genai.Tool)
	}{
		{
			name:  "Empty Declarations",
			input: nil,
			validate: func(t *testing.T, result []*genai.Tool) {
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
		{
			name: "Valid Tool Declaration",
			input: []*tools.ToolDeclaration{
				{
					Name:        "get_weather",
					Description: "Gets the current weather",
					Parameters: &tools.Schema{
						Type: "object",
						Properties: map[string]*tools.Schema{
							"location": {Type: "string"},
						},
					},
				},
			},
			validate: func(t *testing.T, result []*genai.Tool) {
				if len(result) != 1 || len(result[0].FunctionDeclarations) != 1 {
					t.Fatalf("expected 1 tool with 1 declaration, got %v", result)
				}
				decl := result[0].FunctionDeclarations[0]
				if decl.Name != "get_weather" {
					t.Errorf("expected name get_weather, got %s", decl.Name)
				}
				if decl.Description != "Gets the current weather" {
					t.Errorf("expected correct description, got %s", decl.Description)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toSDKTool(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestApplyThinkingBudget(t *testing.T) {

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	client := &Client{eventBus: bus}
	ctx := context.Background()

	t.Run("Under Budget", func(t *testing.T) {
		config := &genai.ThinkingConfig{}
		client.applyThinkingBudget(ctx, config, 1000, 2000, "test-model")

		if config.ThinkingBudget == nil || *config.ThinkingBudget != 1000 {
			t.Errorf("expected budget 1000, got %v", config.ThinkingBudget)
		}
	})

	t.Run("Exceeds Max Budget", func(t *testing.T) {
		localBus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
		events.CleanupBus(t, localBus)
		localClient := &Client{eventBus: localBus}

		var mu sync.Mutex
		var publishedEvents []events.Event
		localBus.Subscribe(func(ctx context.Context, e events.Event) {
			mu.Lock()
			defer mu.Unlock()
			publishedEvents = append(publishedEvents, e)
		})

		config := &genai.ThinkingConfig{}
		localClient.applyThinkingBudget(ctx, config, 3000, 1024, "test-model")

		// Flush to ensure the event is processed
		if err := localBus.Flush(context.Background()); err != nil {
			t.Fatalf("failed to flush event bus: %v", err)
		}

		if config.ThinkingBudget == nil || *config.ThinkingBudget != 1024 {
			t.Errorf("expected budget to be capped at 1024, got %v", config.ThinkingBudget)
		}

		mu.Lock()
		count := len(publishedEvents)
		var firstEvent events.Event
		if count > 0 {
			firstEvent = publishedEvents[0]
		}
		mu.Unlock()

		if count != 1 {
			t.Fatalf("expected 1 warning event, got %d", count)
		}

		msgEvent, ok := firstEvent.(events.SystemMessageEvent)
		if !ok || msgEvent.Level != "warning" {
			t.Errorf("expected warning SystemMessageEvent")
		}
	})
}

func TestHandleEmptyContent(t *testing.T) {

	client := &Client{}

	t.Run("FinishReason Other", func(t *testing.T) {
		candidate := &genai.Candidate{
			FinishReason: genai.FinishReasonSafety,
		}
		err := client.handleEmptyContent(candidate)
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "safety") {
			t.Errorf("expected error containing safety, got %v", err)
		}
	})

	t.Run("FinishReason Stop", func(t *testing.T) {
		candidate := &genai.Candidate{
			FinishReason: genai.FinishReasonStop,
		}
		err := client.handleEmptyContent(candidate)
		if err == nil || !strings.Contains(err.Error(), "empty response from api") {
			t.Errorf("expected generic empty response error, got %v", err)
		}
	})
}

func TestHandleNoCandidates(t *testing.T) {

	client := &Client{}

	t.Run("With BlockReason", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
				BlockReason: "OTHER",
			},
		}
		err := client.handleNoCandidates(resp)
		if err == nil || !strings.Contains(err.Error(), "blocked by safety filters") {
			t.Errorf("expected blocked error, got %v", err)
		}
	})

	t.Run("Without BlockReason", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{}
		err := client.handleNoCandidates(resp)
		if err == nil || !strings.Contains(err.Error(), "empty response from api") {
			t.Errorf("expected generic empty response error, got %v", err)
		}
	})
}

func TestFormatFinishError(t *testing.T) {

	client := &Client{}

	t.Run("Without FinishMessage", func(t *testing.T) {
		candidate := &genai.Candidate{
			FinishReason: genai.FinishReasonSafety,
		}
		err := client.formatFinishError(candidate, "test prefix")
		if err == nil || !strings.Contains(err.Error(), "test prefix (Finish Reason: SAFETY)") {
			t.Errorf("expected correctly formatted error, got %v", err)
		}
	})

	t.Run("With FinishMessage", func(t *testing.T) {
		candidate := &genai.Candidate{
			FinishReason:  genai.FinishReasonSafety,
			FinishMessage: "something went wrong",
		}
		err := client.formatFinishError(candidate, "test prefix")
		if err == nil || !strings.Contains(err.Error(), "test prefix (Finish Reason: SAFETY - something went wrong)") {
			t.Errorf("expected correctly formatted error with message, got %v", err)
		}
	})
}

func TestGemini_InternalErrors(t *testing.T) {
	t.Run("Authenticator Error in NewClient", func(t *testing.T) {
		errAuth := &auth.ServiceAccountAuth{KeyFilePath: "non-existent"}
		_, err := NewClient("http://localhost", "gemini-1.5-flash", errAuth)
		if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
			t.Errorf("expected auth error, got %v", err)
		}
	})

	t.Run("Authenticator Error in prepareAuthHeader", func(t *testing.T) {
		errAuth := &auth.ServiceAccountAuth{KeyFilePath: "non-existent"}
		c := &Client{authenticator: errAuth}
		_, err := c.prepareAuthHeader(context.Background())
		if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
			t.Errorf("expected auth error, got %v", err)
		}
	})
}

func TestGemini_EdgeCase_FindInParts(t *testing.T) {
	tests := []struct {
		name  string
		parts []string
		key   string
		want  string
	}{
		{"missing key", []string{"a", "b", "c"}, "d", ""},
		{"key at end", []string{"a", "b", "c"}, "c", ""},
		{"found", []string{"a", "b", "c"}, "a", "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := findInParts(tt.parts, tt.key); got != tt.want {
				t.Errorf("expected %q, got %q", tt.want, got)
			}
		})
	}
}

func TestGemini_EdgeCase_DetermineBackend_VertexAI(t *testing.T) {
	c := &Client{}
	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/publishers/google/models/gemini-1.5-flash"
	backend, project, location, baseURL := c.determineBackend(apiURL)
	if backend != genai.BackendVertexAI {
		t.Errorf("expected VertexAI backend, got %v", backend)
	}
	if project != "my-project" {
		t.Errorf("expected project my-project, got %s", project)
	}
	if location != "us-central1" {
		t.Errorf("expected location us-central1, got %s", location)
	}
	if !strings.HasPrefix(baseURL, "https://us-central1-aiplatform.googleapis.com/") {
		t.Errorf("unexpected baseURL %s", baseURL)
	}
}

func TestGemini_EdgeCase_DetermineBackend_MockURL(t *testing.T) {
	t.Setenv("TELL_ME_MOCK_URL", "http://mock")
	c := &Client{}
	_, _, _, baseURL := c.determineBackend("http://localhost")
	if baseURL != "http://mock" {
		t.Errorf("expected mock baseURL, got %s", baseURL)
	}
}

func TestGemini_EdgeCase_ToSDKContent(t *testing.T) {
	tests := []struct {
		name     string
		input    []*llm.Content
		wantLen  int
		wantText string
	}{
		{
			name:    "nil input",
			input:   []*llm.Content{nil},
			wantLen: 0,
		},
		{
			name:     "empty parts",
			input:    []*llm.Content{{Role: "user", Parts: []*llm.Part{}}},
			wantLen:  1,
			wantText: "[empty]",
		},
	}

	c := &Client{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := c.toSDKContent(context.Background(), tt.input, nil)
			if len(res) != tt.wantLen {
				t.Errorf("len: got %d, want %d", len(res), tt.wantLen)
			}
			if tt.wantText != "" && len(res) > 0 && res[0].Parts[0].Text != tt.wantText {
				t.Errorf("text: got %s, want %s", res[0].Parts[0].Text, tt.wantText)
			}
		})
	}
}

func TestGemini_ResetConnections(t *testing.T) {
	t.Run("initialized client", func(t *testing.T) {
		// Use a Vertex AI style URL to avoid mandatory API key check in SDK for Google AI backend
		apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
		authenticator := &auth.BearerAuth{Token: "test-token"}
		client, err := NewClient(apiURL, "gemini-1.5-flash", authenticator)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		// This should not panic and should access the transport under lock
		client.ResetConnections()
	})

	t.Run("nil transport safety", func(t *testing.T) {
		// Create a client directly with a nil transport (zero value)
		c := &Client{}

		// This should not panic because of the internal nil check and mutex handling
		c.ResetConnections()
	})
}

func TestGemini_ResetConnections_ThreadSafety(t *testing.T) {
	// Use a Vertex AI style URL to avoid mandatory API key check in SDK for Google AI backend
	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.BearerAuth{Token: "test-token"}
	client, err := NewClient(apiURL, "gemini-1.5-flash", authenticator)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client.ResetConnections()
		}()
	}
	wg.Wait()
}

func TestNewClient_Options(t *testing.T) {
	apiURL := "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	authenticator := &auth.BearerAuth{Token: "test-token"}

	c, err := NewClient(
		apiURL,
		"gemini-1.5-flash",
		authenticator,
		WithTimeout(10*time.Second),
		WithHeaders(map[string]string{"X-Test": "val"}),
		WithSearch(true),
		WithThinking(100, "high", 200),
		WithLogger(&ports.NoOpLogger{}),
	)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	if c.timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.timeout)
	}
	if c.headers["X-Test"] != "val" {
		t.Errorf("expected header X-Test=val, got %s", c.headers["X-Test"])
	}
	if !c.useSearch {
		t.Error("expected useSearch to be true")
	}
	if c.thinkingBudget != 100 || c.thinkingLevel != "high" || c.maxThinkingBudget != 200 {
		t.Errorf("unexpected thinking config: budget=%d, level=%s, max=%d", c.thinkingBudget, c.thinkingLevel, c.maxThinkingBudget)
	}
	if _, ok := c.logger.(*ports.NoOpLogger); !ok {
		t.Error("expected logger to be NoOpLogger")
	}
}
