// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/stretchr/testify/assert"
)

func TestContextManager_PipelineMethods(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	history := &mockHistoryManager{}
	factory := &PipelineFactory{Estimator: strategy}
	cm := NewContextManager(strategy, history, nil, factory)

	// Test SetPipeline
	p := NewContextPipeline()
	cm.SetPipeline(p)
	assert.Equal(t, p, cm.Pipeline)

	// Test ensureStandardPipeline - should create if nil
	cm.Pipeline = nil
	limits := events.Limits{MaxHistoryTurns: 10}
	cm.ensureStandardPipeline(limits)
	assert.NotNil(t, cm.Pipeline)
	assert.Greater(t, len(cm.Pipeline.transformers), 0)

	// Test ensureStandardPipeline - should NOT overwrite if non-nil
	existingPipeline := NewContextPipeline()
	cm.Pipeline = existingPipeline
	cm.ensureStandardPipeline(limits)
	assert.Equal(t, existingPipeline, cm.Pipeline)
}

func TestContextManager_RegisterToolRegistry(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	history := &mockHistoryManager{}
	cm := NewContextManager(strategy, history, nil, nil)

	// Case 1: cm.Pipeline is nil. (Assert no panic)
	assert.NotPanics(t, func() {
		cm.RegisterToolRegistry(&mockToolRegistry{})
	})

	// Case 2: cm.Pipeline contains a toolDeclarationGenerator.
	tg := &toolDeclarationGenerator{}
	p := NewContextPipeline(tg)
	cm.SetPipeline(p)
	reg := &mockToolRegistry{}
	cm.RegisterToolRegistry(reg)
	assert.Equal(t, reg, tg.Registry)

	// Case 3: cm.Pipeline contains other transformers but no toolDeclarationGenerator.
	p2 := NewContextPipeline(&emptyTurnFilter{})
	cm.SetPipeline(p2)
	assert.NotPanics(t, func() {
		cm.RegisterToolRegistry(reg)
	})
}

func TestContextManager_GetLimits(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	strategy.SetLimits(1000, 20, 30)
	strategy.SetTieredThreshold(500)
	cm := NewContextManager(strategy, &mockHistoryManager{}, nil, nil)

	limits := cm.GetLimits()
	assert.Equal(t, 1000, limits.MaxHistoryTokens)
	assert.Equal(t, 20, limits.MaxToolTurns)
	assert.Equal(t, 30, limits.MaxHistoryTurns)
	assert.Equal(t, 500, limits.TieredThreshold)
}

func TestContextManager_Summarize(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	history := &mockHistoryManager{}
	cm := NewContextManager(strategy, history, nil, nil)

	ctx := context.Background()
	contents := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}

	// Case 1: cm.Summarizer is nil.
	summary, metrics, err := cm.Summarize(ctx, contents, "focus")
	assert.NoError(t, err)
	assert.Empty(t, summary)
	assert.Nil(t, metrics)

	// Case 2: cm.Summarizer is present.
	mockSum := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "summary result", &llm.Metrics{PromptTokens: 10}, nil
		},
	}
	cm.Summarizer = mockSum
	summary, metrics, err = cm.Summarize(ctx, contents, "focus")
	assert.NoError(t, err)
	assert.Equal(t, "summary result", summary)
	assert.NotNil(t, metrics)
	assert.Equal(t, int32(10), metrics.PromptTokens)
}

func TestContextManager_SummarizeRange(t *testing.T) {
	counter := &mockTokenCounter{}
	strategy := NewContextStrategy(counter, nil)
	history := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
		},
	}
	cm := NewContextManager(strategy, history, nil, nil)

	ctx := context.Background()

	// Case 1: Summarizer is nil
	_, _, err := cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Setup Summarizer
	mockSum := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "range summary", &llm.Metrics{PromptTokens: 5}, nil
		},
	}
	cm.Summarizer = mockSum

	// Case 2: Success
	msg, metrics, err := cm.SummarizeRange(ctx, 1, "focus")
	assert.NoError(t, err)
	assert.Contains(t, msg, "Summarized the first 1 turns")
	assert.NotNil(t, metrics)
	assert.Equal(t, int32(5), metrics.PromptTokens)

	// Case 3: History too short
	history.contents = nil
	msg, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.NoError(t, err)
	assert.Equal(t, "History is too short to summarize yet.", msg)

	// Case 4: History changed during summarization (shortened)
	history.contents = []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	}
	mockSum.summarizeFn = func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		history.contents = history.contents[:1]
		return "late summary", nil, nil
	}
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "history was pruned")

	// Case 5: History content changed during summarization
	history.contents = []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	}
	mockSum.summarizeFn = func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		history.contents[0] = llm.CloneContent(history.contents[0])
		history.contents[0].Parts[0].Text = "changed"
		return "late summary", nil, nil
	}
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "history content changed")

	// Case 6: Safety limit exceeded
	history.contents = []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	}
	counter.tokens = 1000
	strategy.SetContextWindow(500)
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the safety limit")

	// Reset state
	counter.tokens = 0
	strategy.SetContextWindow(1000000)

	// Case 7: Summarizer returns error (Transient)
	history.contents = []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	}
	mockSum.summarizeFn = func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "", nil, fmt.Errorf("%w: transient fail", llm.ErrTransient)
	}
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Case 8: Summarizer returns error (Fatal)
	mockSum.summarizeFn = func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "", nil, fmt.Errorf("fatal fail")
	}
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Case 9: Event publishing
	bus := events.NewSimpleEventBus()
	defer func() {
		if err := bus.Shutdown(ctx); err != nil {
			t.Errorf("failed to shutdown event bus: %v", err)
		}
	}()
	cm.Events = bus
	received := false
	var mu sync.Mutex
	bus.Subscribe(func(e events.Event) {
		if _, ok := e.(events.SystemMessageEvent); ok {
			mu.Lock()
			received = true
			mu.Unlock()
		}
	})
	mockSum.summarizeFn = func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary", nil, nil
	}
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.NoError(t, err)
	_ = bus.Flush(ctx)
	mu.Lock()
	assert.True(t, received)
	mu.Unlock()

	// Case 10: finalizeSummarization fails
	history.setContentsErr = fmt.Errorf("persist fail")
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	history.setContentsErr = nil

	// Case 11: finalizeSummarization fails (Transient)
	history.setContentsErr = fmt.Errorf("%w: persist fail transient", llm.ErrTransient)
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	history.setContentsErr = nil
}

func TestContextManager_Prepare_ClonesContent(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	originalContent := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "original"}},
	}
	history := &mockHistoryManager{
		contents: []*llm.Content{originalContent},
	}
	cm := NewContextManager(strategy, history, nil, nil)

	ctx := context.Background()
	preparedHistory, _, err := cm.Prepare(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, preparedHistory, 1)

	// Verify it's a deep copy: modifying original should not affect preparedHistory
	originalContent.Parts[0].Text = "modified"
	assert.NotEqual(t, originalContent.Parts[0].Text, preparedHistory[0].Parts[0].Text)
	assert.Equal(t, "original", preparedHistory[0].Parts[0].Text)
}
