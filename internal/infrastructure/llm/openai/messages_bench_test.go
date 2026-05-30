// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"fmt"
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
	"The Anthropic Messages API requires tool_use and tool_result blocks to have non-empty IDs. The fail-fast guard in partToContentBlock rejects empty IDs to prevent silent payload corruption.",
}

// makeTextOnlyContentsOpenAI creates n Contents with text-only Parts, alternating "user"/"model" roles.
// Each Content has (i%3)+1 Parts using deterministic samples from textSamples.
// No TransientParts are set.
func makeTextOnlyContentsOpenAI(n int) []*llm.Content {
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

// makeMixedPartsContentsOpenAI creates n Contents exercising OpenAI-specific
// conversion branches in toStandardMessages:
//
//   - Tool-response entries (role "tool"): every 3rd non-system entry.
//     Exercises partitionParts → appendToolResponseMessages.
//   - Model entries with thinking (role "model"): every 2nd non-tool entry.
//     Exercises classifyParts reasoning branch (p.IsThought → reasoningParts
//     or <thought> wrapping, depending on IsDeepSeek).
//   - Model entries with function calls (role "model"): remaining model entries.
//     Exercises classifyParts tool-call branch (p.FunctionCall != nil).
//   - User entries (role "user"): baseline path — one text Part.
//
// System entries are included when n >= 5 (two at the start), leaving at least
// 3 non-system entries for meaningful role distribution.
func makeMixedPartsContentsOpenAI(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	hasSystemEntries := n >= 5
	sysCount := 0
	if hasSystemEntries {
		sysCount = 2
	}

	modelCount := 0
	for i := 0; i < n; i++ {
		if i < sysCount {
			systemParts := make([]*llm.Part, 2)
			systemParts[0] = &llm.Part{Text: "You are a helpful coding assistant."}
			systemParts[1] = &llm.Part{Text: fmt.Sprintf("Context %d: %s", i, textSamples[i%len(textSamples)])}
			contents[i] = &llm.Content{
				Role:  "system",
				Parts: systemParts,
			}
			continue
		}

		nonSysIdx := i - sysCount

		// Every 3rd entry is a tool-response
		if nonSysIdx%3 == 0 {
			sampleIdx := i % len(textSamples)
			idPrefix := fmt.Sprintf("tool_resp_%c%d", 'a'+rune(i%26), i/26)
			contents[i] = &llm.Content{
				Role: "tool",
				Parts: []*llm.Part{
					{
						FunctionResponse: &llm.FunctionResponse{
							ID:   idPrefix,
							Name: "read_file",
							Response: map[string]any{
								"result": textSamples[sampleIdx],
							},
						},
					},
				},
			}
			continue
		}

		// Non-tool entries alternate user/model
		nonToolIdx := nonSysIdx - (nonSysIdx / 3) // index among non-tool entries
		if nonToolIdx%2 == 0 {
			// User entry — baseline path
			sampleIdx := i % len(textSamples)
			contents[i] = &llm.Content{
				Role: "user",
				Parts: []*llm.Part{
					{Text: textSamples[sampleIdx]},
				},
			}
		} else {
			// Model entry
			modelCount++
			sampleIdx := i % len(textSamples)
			idPrefix := fmt.Sprintf("call_%c%d", 'a'+rune(i%26), i/26)

			if modelCount%2 == 1 {
				// Thinking model entry — exercises classifyParts reasoning branch
				contents[i] = &llm.Content{
					Role: "model",
					Parts: []*llm.Part{
						{Text: textSamples[sampleIdx]},
						{
							IsThought:        true,
							Text:             fmt.Sprintf("Let me analyze the request from sample %d...", i),
							ThoughtSignature: []byte("sig"),
						},
					},
				}
			} else {
				// Function-call model entry — exercises classifyParts tool-call branch
				contents[i] = &llm.Content{
					Role: "model",
					Parts: []*llm.Part{
						{Text: textSamples[sampleIdx]},
						{
							FunctionCall: &llm.FunctionCall{
								ID:   idPrefix,
								Name: "read_file",
								Args: map[string]any{
									"filepath": "internal/domain/llm/types.go",
									"reason":   "review type definitions",
								},
							},
						},
					},
				}
			}
		}
	}
	return contents
}

// BenchmarkOpenAIMessageConversion measures the throughput of toStandardMessages
// across different input profiles and sizes.
func BenchmarkOpenAIMessageConversion(b *testing.B) {
	b.Run("Small/TextOnly", func(b *testing.B) {
		c := NewClient("", "gpt-4", nil)
		ctx := context.Background()
		input := makeTextOnlyContentsOpenAI(1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msgs, err := c.toStandardMessages(ctx, input, nil)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Small/MixedParts", func(b *testing.B) {
		c := NewClient("", "gpt-4", nil)
		ctx := context.Background()
		input := makeMixedPartsContentsOpenAI(1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msgs, err := c.toStandardMessages(ctx, input, nil)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Large/TextOnly", func(b *testing.B) {
		c := NewClient("", "gpt-4", nil)
		ctx := context.Background()
		input := makeTextOnlyContentsOpenAI(100)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msgs, err := c.toStandardMessages(ctx, input, nil)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Large/MixedParts", func(b *testing.B) {
		c := NewClient("", "gpt-4", nil)
		ctx := context.Background()
		input := makeMixedPartsContentsOpenAI(100)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msgs, err := c.toStandardMessages(ctx, input, nil)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})
}
