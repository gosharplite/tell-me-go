// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agenttest

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestMockSummarizer_Summarize_Default(t *testing.T) {
	t.Parallel()

	m := &MockSummarizer{}
	summary, metrics, err := m.Summarize(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "summary" {
		t.Errorf("got summary %q; want %q", summary, "summary")
	}
	if metrics == nil {
		t.Fatal("got nil metrics; want non-nil")
	}
}

func TestMockSummarizer_Summarize_Override(t *testing.T) {
	t.Parallel()

	wantSummary := "custom summary"
	wantMetrics := &llm.Metrics{PromptTokens: 42}
	wantErr := errors.New("summarize failed")

	m := &MockSummarizer{
		SummarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return wantSummary, wantMetrics, wantErr
		},
	}

	summary, metrics, err := m.Summarize(context.Background(), nil, "")
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v; want %v", err, wantErr)
	}
	if summary != wantSummary {
		t.Errorf("got summary %q; want %q", summary, wantSummary)
	}
	if metrics != wantMetrics {
		t.Fatalf("got metrics %+v; want %+v", metrics, wantMetrics)
	}
}

func TestMockSummarizer_SetSummarizeFn(t *testing.T) {
	t.Parallel()

	m := &MockSummarizer{}
	custom := func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "set", nil, nil
	}
	m.SetSummarizeFn(custom)

	summary, _, err := m.Summarize(context.Background(), nil, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "set" {
		t.Errorf("got summary %q; want %q", summary, "set")
	}
}
