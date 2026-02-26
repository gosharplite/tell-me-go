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

func validateStreamAuth(t *testing.T, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test" {
		t.Errorf("expected bearer token, got '%s'", r.Header.Get("Authorization"))
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
	t.Parallel()
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
	t.Parallel()
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
	client, err := NewClient(
		apiURL,
		"test-model",
		authenticator,
		tt.thinkingBudget,
		tt.thinkingBudgetSeverity,
		0,
		tt.systemInstruction,
		false,
		events.NewSimpleEventBus(),
		5*time.Second,
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

type streamChatTestCase struct {
	name           string
	mockChunks     []genai.GenerateContentResponse
	expectedText   string
	expectedError  string
	expectedTokens int32
	validateReq    func(t *testing.T, r *http.Request)
}

func TestStreamChat_Scenarios(t *testing.T) {
	t.Parallel()
	tests := []streamChatTestCase{
		{
			name: "Success",
			mockChunks: []genai.GenerateContentResponse{
				{
					Candidates: []*genai.Candidate{
						{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: "Hello "}},
							},
						},
					},
				},
				{
					Candidates: []*genai.Candidate{
						{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: "World!"}},
							},
							FinishReason: genai.FinishReasonStop,
						},
					},
					UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
						CandidatesTokenCount: 2,
						PromptTokenCount:     1,
						TotalTokenCount:      3,
					},
				},
			},
			expectedText:   "Hello World!",
			expectedTokens: 2,
			validateReq:    validateStreamAuth,
		},
		{
			name: "SafetyBlock",
			mockChunks: []genai.GenerateContentResponse{
				{
					PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
						BlockReason: "SAFETY",
					},
				},
			},
			expectedError: "blocked by safety filters",
		},
		{
			name: "FinishReasonSafety",
			mockChunks: []genai.GenerateContentResponse{
				{
					Candidates: []*genai.Candidate{
						{
							Content: &genai.Content{
								Parts: []*genai.Part{{Text: "Some text"}},
							},
							FinishReason: genai.FinishReasonSafety,
						},
					},
				},
			},
			expectedError: "stream interrupted (Finish Reason: SAFETY)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runStreamChatTest(t, tt)
		})
	}
}

func runStreamChatTest(t *testing.T, tt streamChatTestCase) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tt.validateReq != nil {
			tt.validateReq(t, r)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		for _, chunk := range tt.mockChunks {
			data, _ := json.Marshal(chunk)
			fmt.Fprintf(w, "data: %s\r\n\r\n", string(data))
		}
	}))
	t.Cleanup(server.Close)

	apiURL := server.URL + "/aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models"
	client, _ := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

	var receivedText string
	callback := func(c *llm.Content) {
		for _, p := range c.Parts {
			receivedText += p.Text
		}
	}

	metrics, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, callback)
	assertStreamResults(t, tt, receivedText, metrics, err)
}

func assertStreamResults(t *testing.T, tt streamChatTestCase, receivedText string, metrics *llm.Metrics, err error) {
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
		t.Fatalf("StreamChat failed: %v", err)
	}

	if receivedText != tt.expectedText {
		t.Errorf("expected %q, got %q", tt.expectedText, receivedText)
	}

	if tt.expectedTokens > 0 {
		if metrics == nil || metrics.ResponseTokens != tt.expectedTokens {
			t.Errorf("expected %d response tokens, got %v", tt.expectedTokens, metrics)
		}
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
	client, _ := NewClient(apiURL, "test-model", authenticator, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)

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
	client, err := NewClient(apiURL, "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, events.NewSimpleEventBus(), 5*time.Second)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	images, err := client.GenerateImages(context.Background(), "imagen-3.0", "a cat", "image/png")
	assertGenerateImagesResults(t, tt, images, err)
}

func handleGenerateImagesMock(t *testing.T, w http.ResponseWriter, r *http.Request, tt generateImagesTestCase) {
	if tt.mockError != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, tt.mockError.Error())
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
	t.Parallel()

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
	t.Parallel()

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
			t.Parallel()
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
	t.Parallel()

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
	t.Parallel()

	bus := events.NewSimpleEventBus()
	client := &Client{eventBus: bus}

	t.Run("Under Budget", func(t *testing.T) {
		config := &genai.ThinkingConfig{}
		client.applyThinkingBudget(config, 1000, 2000, "test-model")

		if config.ThinkingBudget == nil || *config.ThinkingBudget != 1000 {
			t.Errorf("expected budget 1000, got %v", config.ThinkingBudget)
		}
	})

	t.Run("Exceeds Max Budget", func(t *testing.T) {
		localBus := events.NewSimpleEventBus()
		localClient := &Client{eventBus: localBus}

		var mu sync.Mutex
		var publishedEvents []events.Event
		localBus.Subscribe(func(e events.Event) {
			mu.Lock()
			defer mu.Unlock()
			publishedEvents = append(publishedEvents, e)
		})

		config := &genai.ThinkingConfig{}
		localClient.applyThinkingBudget(config, 3000, 1024, "test-model")

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
	t.Parallel()

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
	t.Parallel()

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

func TestHandleSafetyBlock(t *testing.T) {
	t.Parallel()

	client := &Client{}

	t.Run("With BlockReason", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			PromptFeedback: &genai.GenerateContentResponsePromptFeedback{
				BlockReason: "SAFETY",
			},
		}
		err := client.handleSafetyBlock(resp)
		if err == nil || !strings.Contains(err.Error(), "blocked by safety filters") {
			t.Errorf("expected blocked error, got %v", err)
		}
	})

	t.Run("Without BlockReason", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{}
		err := client.handleSafetyBlock(resp)
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
}

func TestFormatFinishError(t *testing.T) {
	t.Parallel()

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
		_, err := NewClient("http://localhost", "gemini-1.5-flash", errAuth, 0, "", 0, "", false, nil, 0)
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

func TestGemini_EdgeCases(t *testing.T) {
	t.Run("findInParts", func(t *testing.T) {
		parts := []string{"a", "b", "c"}
		if findInParts(parts, "d") != "" {
			t.Error("expected empty string for missing key")
		}
		if findInParts(parts, "c") != "" {
			t.Error("expected empty string for key at end")
		}
		if findInParts(parts, "a") != "b" {
			t.Errorf("expected b, got %s", findInParts(parts, "a"))
		}
	})

	t.Run("determineBackend Vertex AI", func(t *testing.T) {
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
	})

	t.Run("determineBackend Mock URL", func(t *testing.T) {
		t.Setenv("TELL_ME_MOCK_URL", "http://mock")
		c := &Client{}
		_, _, _, baseURL := c.determineBackend("http://localhost")
		if baseURL != "http://mock" {
			t.Errorf("expected mock baseURL, got %s", baseURL)
		}
	})

	t.Run("toSDKContent nil input", func(t *testing.T) {
		c := &Client{}
		res := c.toSDKContent(context.Background(), []*llm.Content{nil}, nil)
		if len(res) != 0 {
			t.Errorf("expected 0 contents, got %d", len(res))
		}
	})

	t.Run("toSDKContent empty parts", func(t *testing.T) {
		c := &Client{}
		res := c.toSDKContent(context.Background(), []*llm.Content{{Role: "user", Parts: []*llm.Part{}}}, nil)
		if len(res) != 1 {
			t.Errorf("expected 1 content for empty parts (defensive), got %d", len(res))
		}
		if res[0].Parts[0].Text != "[empty]" {
			t.Errorf("expected [empty] part, got %s", res[0].Parts[0].Text)
		}
	})
}

func TestStreamChat_InternalError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal server error")
	}))
	defer server.Close()

	t.Setenv("TELL_ME_MOCK_URL", server.URL)
	t.Setenv("GOOGLE_API_KEY", "dummy")
	client, _ := NewClient("http://localhost/v1", "test-model", &auth.VertexAuth{Token: "test"}, 0, "", 0, "", false, nil, 5*time.Second)

	_, err := client.StreamChat(context.Background(), []*llm.Content{}, nil, nil, func(c *llm.Content) {})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
