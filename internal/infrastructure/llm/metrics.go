// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"google.golang.org/genai"
)

// getMetrics extracts metrics from a GenAI response.
func getMetrics(resp *genai.GenerateContentResponse, duration float64) *llm.Metrics {
	m := &llm.Metrics{
		Duration: duration,
	}

	if resp != nil && resp.UsageMetadata != nil {
		um := resp.UsageMetadata
		m.CachedTokens = um.CachedContentTokenCount
		m.PromptTokens = um.PromptTokenCount
		m.ResponseTokens = um.CandidatesTokenCount
		m.TotalTokens = um.TotalTokenCount

		// Map native thinking tokens from SDK
		if um.ThoughtsTokenCount > 0 {
			m.ThinkingTokens = um.ThoughtsTokenCount
		}
	}

	// Extract search count
	if resp != nil && len(resp.Candidates) > 0 {
		gm := resp.Candidates[0].GroundingMetadata
		if gm != nil {
			m.SearchQueries = len(gm.WebSearchQueries)
		}
	}

	return m
}
