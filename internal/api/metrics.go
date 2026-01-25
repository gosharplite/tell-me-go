// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package api

import (
	"google.golang.org/genai"
)

// Metrics represents the token usage and timing for a single API turn.
type Metrics struct {
	Timestamp      string
	CachedTokens   int32
	PromptTokens   int32
	ResponseTokens int32
	TotalTokens    int32
	ThinkingTokens int32
	SearchQueries  int
	Duration       float64
}

// GetMetrics extracts metrics from a GenAI response.
func GetMetrics(resp *genai.GenerateContentResponse, duration float64) *Metrics {
	m := &Metrics{
		Duration: duration,
	}

	if resp != nil && resp.UsageMetadata != nil {
		um := resp.UsageMetadata
		m.CachedTokens = um.CachedContentTokenCount
		m.PromptTokens = um.PromptTokenCount
		m.ResponseTokens = um.CandidatesTokenCount
		m.TotalTokens = um.TotalTokenCount
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
