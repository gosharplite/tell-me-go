// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// textSamples provides realistic conversation snippets for benchmark input generation.
var textSamples = []string{
	"Let's review the current architecture. We have a hexagonal design with domain, infrastructure, and application layers. The ports package defines all interfaces consumed by domain services.",
	"I found a bug in the summarizer — it wasn't stripping inline data before calling the LLM, causing INVALID_ARGUMENT errors from the API. The fix converts binary parts to text representations.",
	"The modularity constraints require that no infrastructure package imports from another infrastructure package directly. Everything flows through the domain ports.",
	"We need to add a new event type for tracking summarization metrics. The events package already has UsageMetricsEvent; we just need to set IsSummary to true on the metrics.",
	"Running the test suite now. All 342 tests pass, coverage is at 87.3%. The new summarizer tests cover the empty response, transient error, and permanent error paths.",
	"The session state shows 3 pending tasks: implement the focus parameter parsing, add benchmark tests for prepareSummarizerInput, and verify the gateway mock handles nil resolvers.",
	"I refactored the context pruning logic to use the new summarizer instead of simply dropping old turns. This preserves architectural context across long sessions.",
	"The key decision here is to keep Content immutable at the prepareSummarizerInput boundary. It allocates new Content and Part objects rather than mutating the input slice.",
	"Verification results from the last CI run: all unit tests pass, race detector is clean, and the benchmark shows prepareSummarizerInput processes 20 mixed contents in under 50µs.",
}

// makeTextOnlyContents creates n Contents with text-only Parts, alternating "user"/"model" roles.
// Each Content has 1-3 text Parts using deterministic samples from textSamples.
func makeTextOnlyContents(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}

		// Deterministic part count: 1, 2, or 3 parts based on position
		numParts := (i % 3) + 1

		parts := make([]*llm.Part, numParts)
		for j := 0; j < numParts; j++ {
			sampleIdx := (i + j) % len(textSamples)
			parts[j] = &llm.Part{Text: textSamples[sampleIdx]}
		}

		contents[i] = &llm.Content{
			Role:  role,
			Parts: parts,
		}
	}
	return contents
}

// makeMixedContentsForSummary creates n Contents with all four Part types that exercise
// every branch of transformPartToText: Text, FunctionCall, FunctionResponse, and InlineData.
func makeMixedContentsForSummary(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	for i := 0; i < n; i++ {
		role := "user"
		if i%2 == 1 {
			role = "model"
		}

		sampleIdx := i % len(textSamples)

		parts := []*llm.Part{
			// Branch 1: p.Text != ""
			{Text: textSamples[sampleIdx]},
			// Branch 2: p.FunctionCall != nil
			{
				FunctionCall: &llm.FunctionCall{
					ID:   "call_" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
					Name: "read_file",
					Args: map[string]any{
						"filepath": "internal/domain/llm/types.go",
						"reason":   "review type definitions",
					},
				},
			},
			// Branch 3: p.FunctionResponse != nil
			{
				FunctionResponse: &llm.FunctionResponse{
					ID:   "call_" + string(rune('a'+i%26)) + string(rune('0'+i/26)),
					Name: "read_file",
					Response: map[string]any{
						"result": "type Content struct { Role string; Parts []*Part }",
					},
				},
			},
			// Branch 4: p.InlineData != nil
			{
				InlineData: &llm.Blob{
					MIMEType: "image/png",
					Data:     []byte{0x89, 0x50, 0x4E, 0x47},
				},
			},
		}

		contents[i] = &llm.Content{
			Role:  role,
			Parts: parts,
		}
	}
	return contents
}

// BenchmarkSummarizer measures the throughput of prepareSummarizerInput
// across different input profiles.
func BenchmarkSummarizer(b *testing.B) {
	b.Run("Empty", func(b *testing.B) {
		s := &summarizer{}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.prepareSummarizerInput(nil, "")
		}
	})

	b.Run("TextOnly", func(b *testing.B) {
		s := &summarizer{}
		subset := makeTextOnlyContents(20)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.prepareSummarizerInput(subset, "")
		}
	})

	b.Run("MixedParts", func(b *testing.B) {
		s := &summarizer{}
		subset := makeMixedContentsForSummary(20)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.prepareSummarizerInput(subset, "")
		}
	})

	b.Run("WithFocus", func(b *testing.B) {
		s := &summarizer{}
		subset := makeMixedContentsForSummary(20)
		focus := "architecture decisions, modularity constraints"
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = s.prepareSummarizerInput(subset, focus)
		}
	})
}
