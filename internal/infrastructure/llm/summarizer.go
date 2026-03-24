// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// summarizer implements the ports.Summarizer interface using an LLM gateway.
type summarizer struct {
	gateway llm.LLMGateway
	events  events.EventBus
	logger  *slog.Logger
}

// NewSummarizer creates a new summarization service.
func NewSummarizer(g llm.LLMGateway, bus events.EventBus, opts ...summarizerOption) ports.Summarizer {
	s := &summarizer{
		gateway: g,
		events:  bus,
		logger:  slog.Default(),
	}

	for _, opt := range opts {
		opt(s)
	}

	return s
}

// summarizerOption defines a functional option for configuring the summarizer.
type summarizerOption func(*summarizer)

// WithLogger sets the logger for the summarizer.
func WithLogger(l *slog.Logger) summarizerOption {
	return func(s *summarizer) {
		s.logger = l
	}
}

// Summarize uses the LLM to compress a subset of history.
func (s *summarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
	startTime := time.Now()

	summarizerInput := s.prepareSummarizerInput(subset, focus)

	// We need a resolver for the gateway call, but since we've stripped binary data, it's mostly for satisfying the interface.
	respContent, metrics, err := s.gateway.Generate(ctx, summarizerInput, nil, nil)
	duration := time.Since(startTime)

	if err != nil {
		s.logger.Error("Summarization turn failed",
			slog.Int("turns_summarized", len(subset)),
			slog.Int64("duration_ms", duration.Milliseconds()),
			slog.String("error", err.Error()),
		)
		if llm.IsTransient(err) {
			return "", nil, fmt.Errorf("%w: summarization failed due to transient issue", err)
		}
		return "", nil, fmt.Errorf("%w: summarization failed permanently", err)
	}

	s.logger.Info("Summarization turn completed successfully",
		slog.Int("turns_summarized", len(subset)),
		slog.Int64("duration_ms", duration.Milliseconds()),
	)

	s.emitSummarizationMetrics(ctx, metrics, startTime)

	text, err := s.validateSummarizationResponse(respContent)
	if err != nil {
		return "", nil, err
	}

	return text, metrics, nil
}

// prepareSummarizerInput transforms history to text-only to avoid INVALID_ARGUMENT and appends the summarization prompt.
func (s *summarizer) prepareSummarizerInput(subset []*llm.Content, focus string) []*llm.Content {
	input := make([]*llm.Content, 0, len(subset)+1)
	for _, c := range subset {
		content := &llm.Content{Role: c.Role, Parts: make([]*llm.Part, 0, len(c.Parts))}
		for _, p := range c.Parts {
			s.transformPartToText(content, p)
		}
		input = append(input, content)
	}

	prompt := summarizationPrompt
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
func (s *summarizer) transformPartToText(content *llm.Content, p *llm.Part) {
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
func (s *summarizer) emitSummarizationMetrics(ctx context.Context, metrics *llm.Metrics, start time.Time) {
	if s.events != nil && metrics != nil {
		metrics.IsSummary = true
		evt := events.UsageMetricsEvent{
			Context:   ctx,
			Metrics:   metrics,
			StartTime: start,
		}
		if err := events.SafePublish(ctx, s.events, evt); err != nil {
			if !errors.Is(err, events.ErrBusNotInitialized) {
				s.logger.Error("event_publish_failed",
					slog.String("event_type", string(evt.Type())),
					slog.Any("error", err))
			}
		}
	}
}

// validateSummarizationResponse ensures the LLM returned valid, non-empty content.
func (s *summarizer) validateSummarizationResponse(respContent *llm.Content) (string, error) {
	if respContent == nil || len(respContent.Parts) == 0 || respContent.Parts[0].Text == "" {
		return "", fmt.Errorf("summarization returned empty content")
	}
	return respContent.Parts[0].Text, nil
}

// summarizationPrompt is the system instruction for history compression.
const summarizationPrompt = `You are a conversation compressor. Summarize the provided history into a concise but comprehensive state summary.

Preserve the following critical context:
1. **Architecture & Modularity**: Current decisions, project structure, package responsibilities, key dependencies, and modularity constraints.
2. **Bug Contexts & Fixes**: Specifics of bugs encountered (e.g., memory optimizations in monitoring, deadlocks in llmcoord) and how they were resolved.
3. **Key Resolution Steps**: Significant file modifications and the logical rationale (e.g., "mutating the original pointer" strategy).
4. **Verification Results**: Outcomes of tests (unit, E2E), benchmarks, and health checks.
5. **Session State**: Successfully executed commands, pending tasks (from task list), and unresolved issues.

Discard:
1. Verbatim file contents, boilerplate code, or redundant tool logs.
2. Interim "trial and error" failures that do not impact the final resolution.
3. Repetitive tool metadata and verbose output.

Output a structured Markdown summary using headers for clarity. This summary will replace the summarized turns in the conversation history to provide a clean slate for the next phase while maintaining full architectural awareness.
`
