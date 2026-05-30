// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package anthropic

import (
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
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

// makeTextOnlyContentsAnthropic creates n Contents with text-only Parts, alternating "user"/"model" roles.
// Each Content has (i%3)+1 Parts using deterministic samples from textSamples.
// No TransientParts are set.
func makeTextOnlyContentsAnthropic(n int) []*llm.Content {
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

// makeMixedPartsContentsAnthropic creates n Contents exercising every branch of partToContentBlock.
//
// System entries (exercising appendSystemContent) are only included when n >= 4 to guarantee
// at least 2 non-system entries for meaningful role alternation. Remaining entries alternate
// "user"/"model" roles and each gets:
//   - Part 1: Text — exercises p.Text != "" branch
//   - Part 2: FunctionCall with non-empty ID — exercises p.FunctionCall != nil branch
//   - Part 3: FunctionResponse with non-empty ID — exercises p.FunctionResponse != nil branch
//   - Part 4: (model-role only) IsThought=true with Text and ThoughtSignature — exercises thinking branch
//
// Thinking parts are placed only on "model"-role Contents because partToContentBlock returns
// ok=false for thinking parts on non-assistant roles.
func makeMixedPartsContentsAnthropic(n int) []*llm.Content {
	contents := make([]*llm.Content, n)
	hasSystemEntries := n >= 4
	sysCount := 0
	if hasSystemEntries {
		sysCount = 2
	}
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

		role := "user"
		if nonSysIdx%2 == 1 {
			role = "model"
		}

		sampleIdx := i % len(textSamples)
		idPrefix := fmt.Sprintf("call_%c%d", 'a'+rune(i%26), i/26)

		parts := []*llm.Part{
			// Branch 1: p.Text != ""
			{Text: textSamples[sampleIdx]},
			// Branch 2: p.FunctionCall != nil — non-empty ID required (fail-fast guard)
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
			// Branch 3: p.FunctionResponse != nil — non-empty ID required (fail-fast guard)
			{
				FunctionResponse: &llm.FunctionResponse{
					ID:   idPrefix,
					Name: "read_file",
					Response: map[string]any{
						"result": "type Content struct { Role string; Parts []*Part }",
					},
				},
			},
		}

		// Branch 4: p.IsThought && role == "assistant" — only valid on model/assistant roles
		if role == "model" {
			parts = append(parts, &llm.Part{
				IsThought:        true,
				Text:             fmt.Sprintf("Let me analyze the request from sample %d...", i),
				ThoughtSignature: []byte("sig"),
			})
		}

		contents[i] = &llm.Content{
			Role:  role,
			Parts: parts,
		}
	}
	return contents
}

// BenchmarkAnthropicMessageConversion measures the throughput of toAnthropicMessages
// across different input profiles and sizes.
func BenchmarkAnthropicMessageConversion(b *testing.B) {
	b.Run("Small/TextOnly", func(b *testing.B) {
		c := &client{logger: &ports.NoOpLogger{}}
		input := makeTextOnlyContentsAnthropic(1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, msgs, err := c.toAnthropicMessages(input)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Small/MixedParts", func(b *testing.B) {
		c := &client{logger: &ports.NoOpLogger{}}
		input := makeMixedPartsContentsAnthropic(1)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, msgs, err := c.toAnthropicMessages(input)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Large/TextOnly", func(b *testing.B) {
		c := &client{logger: &ports.NoOpLogger{}}
		input := makeTextOnlyContentsAnthropic(100)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, msgs, err := c.toAnthropicMessages(input)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})

	b.Run("Large/MixedParts", func(b *testing.B) {
		c := &client{logger: &ports.NoOpLogger{}}
		input := makeMixedPartsContentsAnthropic(100)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, msgs, err := c.toAnthropicMessages(input)
			if err != nil {
				b.Fatal(err)
			}
			_ = msgs
		}
	})
}
