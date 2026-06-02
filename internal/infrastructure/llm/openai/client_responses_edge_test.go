// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// getResponsesAPIEdgeGap1And2 covers Gap 1 & 2: Direct content blocks
// without message wrapper + child blocks.
func getResponsesAPIEdgeGap1And2(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "direct_content_blocks_without_message_wrapper",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:       "text",
							OutputText: "direct output text",
						},
						{
							Type: "custom_block",
							Content: []contentBlock{
								{Type: "text", Text: "child block text"},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				var foundDirect, foundChild bool
				for _, part := range resp.Parts {
					if part.Text == "direct output text" {
						foundDirect = true
					}
					if part.Text == "child block text" {
						foundChild = true
					}
				}
				if !foundDirect {
					t.Error("expected 'direct output text' part")
				}
				if !foundChild {
					t.Error("expected 'child block text' part from child Content iteration")
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap3 covers Gap 3: parseResponseToolCalls error
// in else-branch.
func getResponsesAPIEdgeGap3(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "direct_item_with_invalid_tool_calls",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "direct_item",
							ToolCalls: []toolCall{
								{
									ID:   "call_1",
									Type: "function",
									Function: functionCall{
										Name:      "test_tool",
										Arguments: "{invalid json",
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

// getResponsesAPIEdgeGap3b covers the child-content-block error
// propagation path in processDirectOutputItem (responses.go:151-153).
// When a direct output item's type is not a recognised content-block
// type (e.g. "message"), appendPartsFromBlock returns
// errUnhandledBlockType, which processDirectOutputItem suppresses via
// errors.Is. Execution then falls through to the child Content loop.
// That loop has NO sentinel suppression, so an unknown child content
// block type MUST propagate as an error.
func getResponsesAPIEdgeGap3b(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "child_content_block_with_unknown_type_propagates_error",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							// Type "message" is not a known content-block
							// type. appendPartsFromBlock returns
							// errUnhandledBlockType, which
							// processDirectOutputItem suppresses via
							// errors.Is. Execution then falls through
							// to the child Content loop below.
							Type: "message",
							Content: []contentBlock{
								{Type: "text", Text: "this is fine"},
								// This child has an unknown type. The
								// child loop has NO sentinel
								// suppression, so the error MUST
								// propagate.
								{Type: "future_block_type_xyz"},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "unhandled content block type",
		},
	}
}

// getResponsesAPIEdgeGap4 covers Gap 4: Top-level Name/Arguments
// without Function.
func getResponsesAPIEdgeGap4(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_via_name_and_arguments",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "call_name_123",
							Name:      "test_tool",
							Arguments: `{"param": "val"}`,
							// No Function field — uses Name/Arguments directly
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				if len(resp.Parts) != 1 || resp.Parts[0].FunctionCall == nil {
					t.Fatal("expected function call part")
				}
				if resp.Parts[0].FunctionCall.ID != "call_name_123" {
					t.Errorf("expected ID 'call_name_123', got %q", resp.Parts[0].FunctionCall.ID)
				}
				if resp.Parts[0].FunctionCall.Name != "test_tool" {
					t.Errorf("expected name 'test_tool', got %q", resp.Parts[0].FunctionCall.Name)
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap4b covers Gap 4b: Top-level Name/Arguments
// with invalid JSON — appendToolCall error path.
func getResponsesAPIEdgeGap4b(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "top_level_tool_call_via_name_and_invalid_arguments",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type:      "call",
							ID:        "call_name_456",
							Name:      "test_tool",
							Arguments: "{invalid json",
							// No Function field — uses Name/Arguments with broken JSON
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "failed to unmarshal tool arguments",
		},
	}
}

// getResponsesAPIEdgeGap6 covers Gap 6: extractBlockText with map
// missing "value" key.
func getResponsesAPIEdgeGap6(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "text_block_with_map_missing_value_key",
			model:   "gpt-5.4",
			headers: map[string]string{"reasoning_effort": "high"},
			tools:   []*tools.ToolDeclaration{getStandardToolDecl()},
			mockHandler: func(w http.ResponseWriter, r *http.Request) {
				resp := responsesAPIResponse{
					Output: []responseOutputItem{
						{
							Type: "text",
							Text: map[string]interface{}{"other": "not-value"},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				// extractBlockText returns "" for map without "value" key
				// handleTextBlock then checks OutputText (empty) and InputText (empty)
				// No part should be added
				if len(resp.Parts) != 0 {
					t.Errorf("expected 0 parts for map without 'value' key, got %d", len(resp.Parts))
				}
			},
		},
	}
}

// getResponsesAPIEdgeGap7 covers Gap 7: handleRefusalBlock with empty
// Refusal.
func getResponsesAPIEdgeGap7(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "refusal_block_with_empty_text",
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
								Content: []contentBlock{
									{Type: "refusal", Refusal: ""},
									{Type: "text", Text: "fallback text"},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			validate: func(t *testing.T, resp *llm.Content, metrics *llm.Metrics) {
				// Empty refusal should not produce a part; "fallback text" should
				if len(resp.Parts) != 1 || resp.Parts[0].Text != "fallback text" {
					t.Errorf("expected 1 part 'fallback text', got %+v", resp.Parts)
				}
			},
		},
	}
}

// getResponsesAPIEdgeADR022 covers ADR-022: Unknown content block type
// must return an error (fail-loud).
func getResponsesAPIEdgeADR022(t *testing.T) []responsesAPITestCase {
	return []responsesAPITestCase{
		{
			name:    "unknown_content_block_type_returns_error",
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
								Content: []contentBlock{
									{Type: "future_block_type_xyz"},
								},
							},
						},
					},
				}
				_ = json.NewEncoder(w).Encode(resp)
			},
			wantErr: "unhandled content block type",
		},
	}
}

// getResponsesAPIEdgeCases aggregates all edge-case test scenarios.
func getResponsesAPIEdgeCases(t *testing.T) []responsesAPITestCase {
	var cases []responsesAPITestCase
	cases = append(cases, getResponsesAPIEdgeGap1And2(t)...)
	cases = append(cases, getResponsesAPIEdgeGap3(t)...)
	cases = append(cases, getResponsesAPIEdgeGap3b(t)...)
	cases = append(cases, getResponsesAPIEdgeGap4(t)...)
	cases = append(cases, getResponsesAPIEdgeGap4b(t)...)
	cases = append(cases, getResponsesAPIEdgeGap6(t)...)
	cases = append(cases, getResponsesAPIEdgeGap7(t)...)
	cases = append(cases, getResponsesAPIEdgeADR022(t)...)
	return cases
}

func TestResponsesAPIRouting(t *testing.T) {
	t.Run("Routes to /responses when all conditions met", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := responsesAPIResponse{
				ID: "test",
				Output: []responseOutputItem{
					{Type: "text", Text: "OK"},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// All conditions: gpt-5.4 model, tools, reasoning_effort header
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/responses" {
			t.Errorf("Expected path /responses, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions when missing tools", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has gpt-5.4 model and reasoning_effort, but no tools
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		// No tools provided
		_, _, err := client.SendChat(context.Background(), nil, nil, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions when no tools, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions when missing reasoning_effort", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has gpt-5.4 model and tools, but no reasoning_effort header
		client := NewClient(
			server.URL,
			"gpt-5.4",
			&auth.BearerAuth{Token: "test-key"},
			// No WithHeaders
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions when no reasoning_effort, got %s", requestPath)
		}
	})

	t.Run("Routes to /chat/completions for non-gpt-5.4 model", func(t *testing.T) {
		var requestPath string
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestPath = r.URL.Path
			resp := chatResponse{
				ID: "test",
				Choices: []choice{
					{Message: message{Role: "assistant", Content: "OK"}},
				},
				Usage: usage{
					PromptTokens:     1,
					CompletionTokens: 1,
					TotalTokens:      2,
				},
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		}))
		t.Cleanup(func() { server.Close() })

		// Has tools and reasoning_effort, but model is gpt-4 (not gpt-5.4+)
		client := NewClient(
			server.URL,
			"gpt-4",
			&auth.BearerAuth{Token: "test-key"},
			WithHeaders(map[string]string{"reasoning_effort": "high"}),
		)

		toolDecl := &tools.ToolDeclaration{
			Name:        "test_tool",
			Description: "A test tool",
		}

		_, _, err := client.SendChat(context.Background(), nil, []*tools.ToolDeclaration{toolDecl}, nil)
		if err != nil {
			t.Fatalf("SendChat failed: %v", err)
		}

		if requestPath != "/chat/completions" {
			t.Errorf("Expected path /chat/completions for gpt-4 model, got %s", requestPath)
		}
	})
}
