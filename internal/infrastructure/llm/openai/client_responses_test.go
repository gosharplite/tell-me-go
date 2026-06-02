// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	llmerr "github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

type responsesAPITestCase struct {
	name           string
	model          string
	headers        map[string]string
	history        []*llm.Content
	tools          []*tools.ToolDeclaration
	mockHandler    func(w http.ResponseWriter, r *http.Request)
	wantErr        string
	isAPIError     bool
	expectedStatus int
	validate       func(t *testing.T, resp *llm.Content, metrics *llm.Metrics)
}

func TestResponsesAPIEndpoint(t *testing.T) {
	for _, tt := range getResponsesAPITestCases(t) {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runResponsesAPITestCase(t, tt)
		})
	}
}

func runResponsesAPITestCase(t *testing.T, tt responsesAPITestCase) {
	t.Parallel()

	var server *httptest.Server
	var baseURL string
	if tt.mockHandler != nil {
		server = httptest.NewServer(http.HandlerFunc(tt.mockHandler))
		t.Cleanup(func() { server.Close() })
		baseURL = server.URL
	} else {
		baseURL = "http://localhost:9999"
	}

	client := NewClient(baseURL, tt.model, &auth.BearerAuth{Token: "test-key"}, WithHeaders(tt.headers))
	resp, metrics, err := client.SendChat(context.Background(), tt.history, tt.tools, nil)

	if tt.wantErr != "" {
		assertResponsesAPIError(t, tt, err)
		return
	}

	if err != nil {
		t.Fatalf("%s: unexpected error: %v", tt.name, err)
	}

	if tt.validate != nil {
		tt.validate(t, resp, metrics)
	}
}

func assertResponsesAPIError(t *testing.T, tt responsesAPITestCase, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected error containing %q, got nil", tt.name, tt.wantErr)
	}
	if !strings.Contains(err.Error(), tt.wantErr) {
		t.Errorf("%s: expected error containing %q, got %q", tt.name, tt.wantErr, err.Error())
	}
	if tt.isAPIError {
		var apiErr *llmerr.APIError
		if !errors.As(err, &apiErr) {
			t.Errorf("%s: expected llmerr.APIError, got %T", tt.name, err)
		} else if apiErr.Status != tt.expectedStatus {
			t.Errorf("%s: expected status %d, got %d", tt.name, tt.expectedStatus, apiErr.Status)
		}
	}
}

func getResponsesAPITestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getBasicChatTestCases(t)...)
	cases = append(cases, getToolCallTestCases(t)...)
	cases = append(cases, getErrorTestCases(t)...)
	// getStreamingTestCases is currently empty as no streaming cases are in the original list
	cases = append(cases, getStreamingTestCases(t)...)
	return cases
}

func getBasicChatTestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIValidChatTestCases(t)...)
	cases = append(cases, getResponsesAPIMetadataTestCases(t)...)
	cases = append(cases, getResponsesAPIContentBlockTestCases(t)...)
	return cases
}

func getResponsesAPIValidChatTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "valid_request_returns_200",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			history: []*llm.Content{
				{
					Role: "user",
					Parts: []*llm.Part{
						{Text: "Hello, please use the test tool"},
					},
				},
			},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					ID: "resp_test_123",
					Output: []responseOutputItem{
						{Type: "text", Text: "This is a text response"},
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								Content: []contentBlock{
									{Type: "text", Text: "I'm processing your request"},
								},
								ToolCalls: []toolCall{
									{
										ID:   "call_abc123",
										Type: "function",
										Function: functionCall{
											Name:      "test_tool",
											Arguments: `{"param": "value"}`,
										},
									},
								},
							},
						},
					},
					Usage: usage{
						PromptTokens:     50,
						CompletionTokens: 100,
						TotalTokens:      150,
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) < 2 {
					t.Fatalf("Expected at least 2 parts, got %d", len(resp.Parts))
				}
				var foundText, foundToolCall bool
				for _, part := range resp.Parts {
					if part.Text != "" {
						foundText = true
					}
					if part.FunctionCall != nil && part.FunctionCall.Name == "test_tool" {
						foundToolCall = true
					}
				}
				if !foundText || !foundToolCall {
					t.Errorf("missing text or tool call")
				}
			},
		},
	}
}

func getResponsesAPIMetadataTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "usage_accumulated_from_items_over_top_level",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:  "message",
							Usage: &usage{PromptTokens: 10, CompletionTokens: 20, TotalTokens: 30},
						},
					},
					Usage: usage{PromptTokens: 15, CompletionTokens: 35, TotalTokens: 50},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if metrics.TotalTokens != 50 {
					t.Errorf("Expected 50 total tokens, got %d", metrics.TotalTokens)
				}
			},
		},
		{
			name:    "empty_output_array_returns_empty_parts",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					ID:     "resp_test_empty",
					Output: []responseOutputItem{},
					Usage:  usage{PromptTokens: 5, CompletionTokens: 0, TotalTokens: 5},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 0 {
					t.Errorf("Expected 0 parts, got %d", len(resp.Parts))
				}
			},
		},
	}
}

func getResponsesAPIContentBlockTestCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIDirectContentTestCases(t)...)
	cases = append(cases, getResponsesAPIRefusalTestCases(t)...)
	cases = append(cases, getResponsesAPIMixedInputTestCases(t)...)
	cases = append(cases, getResponsesAPITextBlockFallbackTestCases(t)...)
	cases = append(cases, getResponsesAPIEdgeCases(t)...)
	return cases
}

func getResponsesAPIDirectContentTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:        "direct_content_blocks_without_wrapper",
			model:       "gpt-5.4",
			headers:     map[string]string{"reasoning_effort": "high"},
			tools:       []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: getDirectContentMockHandler(),
			validate:    validateDirectContentResponse,
		},
	}
}

func getDirectContentMockHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := responsesAPIResponse{
			Output: []responseOutputItem{
				{
					Type: "message",
					Role: "assistant",
					Content: []contentBlock{
						{Type: "text", Text: "Direct text block"},
						{Type: "thought", Thought: "I'm thinking"},
						{
							Type:  "tool_use",
							Name:  "test_tool",
							ID:    "call_calc_123",
							Input: map[string]interface{}{"operation": "add"},
						},
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func validateDirectContentResponse(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
	var foundText, foundThought, foundToolCall bool
	for _, part := range resp.Parts {
		if part.Text == "Direct text block" && !part.IsThought {
			foundText = true
		}
		if part.Text == "I'm thinking" && part.IsThought {
			foundThought = true
		}
		if part.FunctionCall != nil && part.FunctionCall.Name == "test_tool" {
			foundToolCall = true
		}
	}
	checkDirectContentResults(t, foundText, foundThought, foundToolCall)
}

func checkDirectContentResults(t *testing.T, foundText, foundThought, foundToolCall bool) {
	if !foundText || !foundThought || !foundToolCall {
		t.Errorf("missing components: text=%v, thought=%v, tool=%v", foundText, foundThought, foundToolCall)
	}
}

func getResponsesAPIRefusalTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "refusal_block_handling",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role:    "assistant",
								Content: []contentBlock{{Type: "refusal", Refusal: "I cannot answer that"}},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "I cannot answer that" {
					t.Errorf("unexpected refusal")
				}
			},
		},
	}
}

func getResponsesAPIMixedInputTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "mixed_input_and_output_text_blocks",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:       "text",
							InputText:  "User said: Hello",
							OutputText: "Assistant says: Hi there",
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "Assistant says: Hi there" {
					t.Errorf("expected output text")
				}
			},
		},
	}
}

// getResponsesAPITextBlockFallbackTestCases covers Gaps #14, #15, #16:
// the fallback chains in handleTextBlock and handleThoughtBlock.
//
// Gap #14: handleTextBlock falls back to InputText when both
//
//	extractBlockText(cb.Text) and cb.OutputText are empty.
//
// Gap #15: handleThoughtBlock falls back to cb.Reasoning when
//
//	cb.Thought is empty.
//
// Gap #16: handleThoughtBlock falls back to extractBlockText(cb.Text)
//
//	when both cb.Thought and cb.Reasoning are empty and cb.Type=="thought".
func getResponsesAPITextBlockFallbackTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		// Gap #14: handleTextBlock InputText fallback
		{
			name:    "text_block_falls_back_to_input_text",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "text",
							InputText: "user input text fallback",
							// no Text, no OutputText — forces InputText fallback
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "user input text fallback" {
					t.Errorf("expected InputText fallback 'user input text fallback', got %+v", resp.Parts)
				}
			},
		},
		// Gap #15: handleThoughtBlock Reasoning fallback
		{
			name:    "thought_block_falls_back_to_reasoning_field",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "thought",
							Reasoning: "reasoning from reasoning field",
							// no Thought field — forces Reasoning fallback
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 {
					t.Fatalf("expected 1 part, got %d", len(resp.Parts))
				}
				if !resp.Parts[0].IsThought {
					t.Error("expected IsThought=true")
				}
				if resp.Parts[0].Text != "reasoning from reasoning field" {
					t.Errorf("expected 'reasoning from reasoning field', got %q", resp.Parts[0].Text)
				}
			},
		},
		// Gap #16: handleThoughtBlock type:"thought" text extraction
		{
			name:    "thought_block_falls_back_to_text_field",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "thought",
							Text: "thinking via text field",
							// no Thought, no Reasoning — forces extractBlockText(cb.Text)
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 {
					t.Fatalf("expected 1 part, got %d", len(resp.Parts))
				}
				if !resp.Parts[0].IsThought {
					t.Error("expected IsThought=true")
				}
				if resp.Parts[0].Text != "thinking via text field" {
					t.Errorf("expected 'thinking via text field', got %q", resp.Parts[0].Text)
				}
			},
		},
	}
}

func getStreamingTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{}
}

func getToolCallTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_type_call",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "call",
							ID:   "call_top_123",
							Function: &functionCall{
								Name:      "test_tool",
								Arguments: `{"param": "val"}`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].FunctionCall == nil || resp.Parts[0].FunctionCall.ID != "call_top_123" {
					t.Errorf("unexpected tool call")
				}
			},
		},
		{
			name:    "top_level_tool_call_empty_id_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "call",
							ID:   "", // ← empty ID triggers the guard
							Function: &functionCall{
								Name:      "test_tool",
								Arguments: `{"param": "val"}`,
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "invalid tool payload",
		},
		{
			name:    "top_level_tool_call_empty_id_via_name_args",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "", // ← empty ID
							Name:      "test_tool",
							Arguments: `{"param": "val"}`,
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "invalid tool payload",
		},
	}
}

func getErrorTestCases(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "malformed_json_response_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(`{invalid json}`))
			},
			wantErr: "failed to decode response",
		},
		{
			name:    "http_400_returns_api_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error": {"message": "Invalid request"}}`))
			},
			wantErr:        "api error (status 400)",
			isAPIError:     true,
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:    "tool_response_missing_id_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			history: []*llm.Content{
				{
					Role: "tool",
					Parts: []*llm.Part{
						{
							FunctionResponse: &llm.FunctionResponse{
								ID:       "",
								Name:     "test_tool",
								Response: map[string]interface{}{"result": "test"},
							},
						},
					},
				},
			},
			wantErr: "invalid tool payload",
		},
		{
			name:    "response_with_invalid_tool_call_arguments_returns_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "message",
							Message: &struct {
								Role      string         `json:"role"`
								Content   []contentBlock `json:"content"`
								ToolCalls []toolCall     `json:"tool_calls"`
							}{
								Role: "assistant",
								ToolCalls: []toolCall{
									{
										ID:   "call_invalid_123",
										Type: "function",
										Function: functionCall{
											Name:      "test_tool",
											Arguments: "{invalid json}",
										},
									},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "failed to unmarshal tool arguments",
		},
	}
}
