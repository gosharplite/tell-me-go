// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"testing"

	"google.golang.org/genai"
)

func TestGetMetrics(t *testing.T) {
	t.Parallel()

	t.Run("NilResponse", func(t *testing.T) {
		m := GetMetrics(nil, 1.5)
		if m.Duration != 1.5 {
			t.Errorf("expected duration 1.5, got %f", m.Duration)
		}
		if m.TotalTokens != 0 {
			t.Errorf("expected 0 tokens, got %d", m.TotalTokens)
		}
	})

	t.Run("FullResponse", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
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
		}

		m := GetMetrics(resp, 2.0)
		if m.CachedTokens != 10 {
			t.Errorf("CachedTokens: got %d, want 10", m.CachedTokens)
		}
		if m.PromptTokens != 20 {
			t.Errorf("PromptTokens: got %d, want 20", m.PromptTokens)
		}
		if m.ResponseTokens != 30 {
			t.Errorf("ResponseTokens: got %d, want 30", m.ResponseTokens)
		}
		if m.TotalTokens != 60 {
			t.Errorf("TotalTokens: got %d, want 60", m.TotalTokens)
		}
		if m.ThinkingTokens != 5 {
			t.Errorf("ThinkingTokens: got %d, want 5", m.ThinkingTokens)
		}
		if m.SearchQueries != 2 {
			t.Errorf("SearchQueries: got %d, want 2", m.SearchQueries)
		}
	})

	t.Run("PartialResponse", func(t *testing.T) {
		resp := &genai.GenerateContentResponse{
			UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
				TotalTokenCount: 100,
			},
			// No candidates
		}
		m := GetMetrics(resp, 1.0)
		if m.TotalTokens != 100 {
			t.Errorf("TotalTokens: got %d, want 100", m.TotalTokens)
		}
		if m.SearchQueries != 0 {
			t.Errorf("SearchQueries: got %d, want 0", m.SearchQueries)
		}
	})
}
