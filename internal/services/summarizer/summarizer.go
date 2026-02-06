// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package summarizer

import (
	"context"
	"fmt"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// Summarizer implements the services.Summarizer interface using an LLM gateway.
type Summarizer struct {
	gateway llm.LLMGateway
	events  events.EventBus
}

// NewSummarizer creates a new summarization service.
func NewSummarizer(g llm.LLMGateway, bus events.EventBus) services.Summarizer {
	return &Summarizer{gateway: g, events: bus}
}

// Summarize uses the LLM to compress a subset of history.
func (s *Summarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
	startTime := time.Now()

	// Transform history to text-only to avoid INVALID_ARGUMENT
	summarizerInput := make([]*llm.Content, len(subset))
	for i, c := range subset {
		summarizerInput[i] = &llm.Content{Role: c.Role}
		for _, p := range c.Parts {
			if p.Text != "" {
				summarizerInput[i].Parts = append(summarizerInput[i].Parts, &llm.Part{Text: p.Text})
			}
			if p.FunctionCall != nil {
				summarizerInput[i].Parts = append(summarizerInput[i].Parts, &llm.Part{
					Text: fmt.Sprintf("[Model called tool: %s with args: %v]", p.FunctionCall.Name, p.FunctionCall.Args),
				})
			}
			if p.FunctionResponse != nil {
				res := p.FunctionResponse.Response["result"]
				summarizerInput[i].Parts = append(summarizerInput[i].Parts, &llm.Part{
					Text: fmt.Sprintf("[Tool %s returned: %v]", p.FunctionResponse.Name, res),
				})
			}
			if p.InlineData != nil {
				summarizerInput[i].Parts = append(summarizerInput[i].Parts, &llm.Part{
					Text: fmt.Sprintf("[Binary Data: %s]", p.InlineData.MIMEType),
				})
			}
		}
	}

	prompt := SummarizationPrompt
	if focus != "" {
		prompt += fmt.Sprintf("\nFocus: %s", focus)
	}
	summarizerInput = append(summarizerInput, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	})

	// We need a resolver for the gateway call, but since we've stripped binary data, it's mostly for satisfying the interface.
	respCh, finalize := s.gateway.Generate(ctx, summarizerInput, nil, nil)
	// Drain the channel; we don't stream summarization to the UI.
	for range respCh {
	}
	respContent, metrics, err := finalize()
	if err != nil {
		if llm.IsTransient(err) {
			return "", nil, fmt.Errorf("%w: summarization failed due to transient issue", err)
		}
		return "", nil, fmt.Errorf("%w: summarization failed permanently", err)
	}

	// Emit metrics to the event bus
	if s.events != nil && metrics != nil {
		metrics.IsSummary = true
		s.events.Publish(events.UsageMetricsEvent{
			Context:   ctx,
			Metrics:   metrics,
			StartTime: startTime,
		})
	}

	if len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", nil, fmt.Errorf("summarization returned empty content")
	}

	return respContent.Parts[0].Text, metrics, nil
}

// SummarizationPrompt is the system instruction for history compression.
const SummarizationPrompt = `You are a conversation compressor. Summarize the provided history into a concise but comprehensive state summary.
Preserve:
1. Current architecture decisions and project structure.
2. Modified files and their high-level changes.
3. Successfully executed commands and their critical results.
4. Unresolved issues or pending tasks from the scratchpad/task list.
Discard:
1. Large file contents or boilerplate code output.
2. Redundant tool call logs.
3. "Trial and error" failures that don't affect the final state.

The output must be a single summary that will replace these turns in the history.
`
