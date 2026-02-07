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

	summarizerInput := s.prepareSummarizerInput(subset, focus)

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

	s.emitSummarizationMetrics(ctx, metrics, startTime)

	text, err := s.validateSummarizationResponse(respContent)
	if err != nil {
		return "", nil, err
	}

	return text, metrics, nil
}

// prepareSummarizerInput transforms history to text-only to avoid INVALID_ARGUMENT and appends the summarization prompt.
func (s *Summarizer) prepareSummarizerInput(subset []*llm.Content, focus string) []*llm.Content {
	input := make([]*llm.Content, len(subset))
	for i, c := range subset {
		input[i] = &llm.Content{Role: c.Role}
		for _, p := range c.Parts {
			s.transformPartToText(input[i], p)
		}
	}

	prompt := SummarizationPrompt
	if focus != "" {
		prompt += fmt.Sprintf("\nFocus: %s", focus)
	}
	input = append(input, &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: prompt}},
	})

	return input
}

// transformPartToText converts a single part into its text representation within a content object.
func (s *Summarizer) transformPartToText(content *llm.Content, p *llm.Part) {
	if p.Text != "" {
		content.Parts = append(content.Parts, &llm.Part{Text: p.Text})
	}
	if p.FunctionCall != nil {
		content.Parts = append(content.Parts, &llm.Part{
			Text: fmt.Sprintf("[Model called tool: %s with args: %v]", p.FunctionCall.Name, p.FunctionCall.Args),
		})
	}
	if p.FunctionResponse != nil {
		res := p.FunctionResponse.Response["result"]
		content.Parts = append(content.Parts, &llm.Part{
			Text: fmt.Sprintf("[Tool %s returned: %v]", p.FunctionResponse.Name, res),
		})
	}
	if p.InlineData != nil {
		content.Parts = append(content.Parts, &llm.Part{
			Text: fmt.Sprintf("[Binary Data: %s]", p.InlineData.MIMEType),
		})
	}
}

// emitSummarizationMetrics publishes usage metrics to the event bus.
func (s *Summarizer) emitSummarizationMetrics(ctx context.Context, metrics *llm.Metrics, start time.Time) {
	if s.events != nil && metrics != nil {
		metrics.IsSummary = true
		s.events.Publish(events.UsageMetricsEvent{
			Context:   ctx,
			Metrics:   metrics,
			StartTime: start,
		})
	}
}

// validateSummarizationResponse ensures the LLM returned valid, non-empty content.
func (s *Summarizer) validateSummarizationResponse(respContent *llm.Content) (string, error) {
	if respContent == nil || len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", fmt.Errorf("summarization returned empty content")
	}
	return respContent.Parts[0].Text, nil
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
