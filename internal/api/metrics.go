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
	if resp == nil || resp.UsageMetadata == nil {
		return nil
	}

	um := resp.UsageMetadata
	m := &Metrics{
		Timestamp:      "", // To be filled by logger
		CachedTokens:   um.CachedContentTokenCount,
		PromptTokens:   um.PromptTokenCount,
		ResponseTokens: um.CandidatesTokenCount,
		TotalTokens:    um.TotalTokenCount,
		Duration:       duration,
	}

	// Extract search count
	if resp.Candidates != nil && len(resp.Candidates) > 0 {
		gm := resp.Candidates[0].GroundingMetadata
		if gm != nil {
			m.SearchQueries = len(gm.WebSearchQueries)
		}
	}

	return m
}

