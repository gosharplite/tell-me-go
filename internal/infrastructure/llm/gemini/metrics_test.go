// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/genai"
)

func TestGetMetrics(t *testing.T) {

	tests := []struct {
		name     string
		resp     *genai.GenerateContentResponse
		duration float64
		want     llm.Metrics
	}{
		{
			name:     "NilResponse",
			resp:     nil,
			duration: 1.5,
			want: llm.Metrics{
				Duration: 1.5,
			},
		},
		{
			name: "FullResponse",
			resp: &genai.GenerateContentResponse{
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					CachedContentTokenCount: 10,
					PromptTokenCount:        20,
					CandidatesTokenCount:    30,
					TotalTokenCount:         60,
					ThoughtsTokenCount:      5,
				},
				Candidates: []*genai.Candidate{
					{
						GroundingMetadata: &genai.GroundingMetadata{
							WebSearchQueries: []string{"q1", "q2"},
						},
					},
				},
			},
			duration: 2.0,
			want: llm.Metrics{
				Duration:       2.0,
				CachedTokens:   10,
				PromptTokens:   20,
				ResponseTokens: 30,
				TotalTokens:    60,
				ThinkingTokens: 5,
				SearchQueries:  2,
			},
		},
		{
			name: "PartialResponse",
			resp: &genai.GenerateContentResponse{
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					TotalTokenCount: 100,
				},
			},
			duration: 1.0,
			want: llm.Metrics{
				Duration:    1.0,
				TotalTokens: 100,
			},
		},
		{
			name: "TrafficTypePriority",
			resp: &genai.GenerateContentResponse{
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					TotalTokenCount: 100,
					TrafficType:     "ON_DEMAND_PRIORITY",
				},
			},
			duration: 1.0,
			want: llm.Metrics{
				Duration:    1.0,
				TotalTokens: 100,
				TrafficType: "ON_DEMAND_PRIORITY",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getMetrics(tt.resp, tt.duration)
			if got == nil {
				t.Fatal("getMetrics returned nil")
			}
			assertMetrics(t, *got, tt.want)
		})
	}
}

func assertMetrics(t *testing.T, got, want llm.Metrics) {
	t.Helper()
	if got.Duration != want.Duration {
		t.Errorf("Duration: got %f, want %f", got.Duration, want.Duration)
	}
	if got.CachedTokens != want.CachedTokens {
		t.Errorf("CachedTokens: got %d, want %d", got.CachedTokens, want.CachedTokens)
	}
	if got.PromptTokens != want.PromptTokens {
		t.Errorf("PromptTokens: got %d, want %d", got.PromptTokens, want.PromptTokens)
	}
	if got.ResponseTokens != want.ResponseTokens {
		t.Errorf("ResponseTokens: got %d, want %d", got.ResponseTokens, want.ResponseTokens)
	}
	if got.TotalTokens != want.TotalTokens {
		t.Errorf("TotalTokens: got %d, want %d", got.TotalTokens, want.TotalTokens)
	}
	if got.ThinkingTokens != want.ThinkingTokens {
		t.Errorf("ThinkingTokens: got %d, want %d", got.ThinkingTokens, want.ThinkingTokens)
	}
	if got.SearchQueries != want.SearchQueries {
		t.Errorf("SearchQueries: got %d, want %d", got.SearchQueries, want.SearchQueries)
	}
	if got.TrafficType != want.TrafficType {
		t.Errorf("TrafficType: got %s, want %s", got.TrafficType, want.TrafficType)
	}
}
