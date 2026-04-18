// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

// MockSummarizer is a test double for ports.Summarizer. The default
// behaviour returns the literal string "summary" with empty metrics;
// override SummarizeFn to script other outcomes.
type MockSummarizer struct {
	SummarizeFn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)
}

func (m *MockSummarizer) Summarize(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
	if m.SummarizeFn != nil {
		return m.SummarizeFn(ctx, subset, focus)
	}
	return "summary", &llm.Metrics{}, nil
}

func (m *MockSummarizer) SetSummarizeFn(fn func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error)) {
	m.SummarizeFn = fn
}
