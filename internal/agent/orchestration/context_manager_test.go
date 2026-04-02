// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestContextManager_PipelineMethods(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})
	history := &mockHistoryManager{}
	factory := &PipelineFactory{Estimator: strategy}
	cm := NewContextManager(strategy, history, nil, factory)

	// Test SetPipeline
	p := NewContextPipeline()
	cm.SetPipeline(p)
	assert.Equal(t, p, cm.Pipeline)
}

func TestContextManager_GetLimits(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})
	strategy.SetLimits(1000, 20, 30)
	strategy.setTieredThreshold(500)
	cm := NewContextManager(strategy, &mockHistoryManager{}, nil, nil)

	limits := cm.GetLimits()
	assert.Equal(t, 1000, limits.MaxHistoryTokens)
	assert.Equal(t, 20, limits.MaxToolTurns)
	assert.Equal(t, 30, limits.MaxHistoryTurns)
	assert.Equal(t, 500, limits.TieredThreshold)
}

func TestContextManager_Summarize(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})
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
	strategy := NewContextStrategy(counter)
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
	strategy.setContextWindow(500)
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the safety limit")

	// Reset state
	counter.tokens = 0
	strategy.setContextWindow(1000000)

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
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	inframock.CleanupBus(t, bus)

	var logBuf syncWriter
	testLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cm.logger = testLogger
	cm.Events = bus

	received := false
	var mu sync.Mutex
	bus.Subscribe(func(ctx context.Context, e events.Event) {
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

	// Verify log if bus was closed during emitSummarizationEvent
	_ = bus.Shutdown(ctx)
	_, _, _ = cm.SummarizeRange(ctx, 1, "")
	output := logBuf.String()
	assert.Contains(t, output, "event_publish_failed")
	assert.Contains(t, output, `"level":"ERROR"`)

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
	strategy := NewContextStrategy(&mockTokenCounter{})
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

func TestContextManager_Reconfigure_UpdatesPipeline(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})
	factory := &PipelineFactory{Estimator: strategy}
	cm := NewContextManager(strategy, &mockHistoryManager{}, nil, factory)

	// Initially not nil because NewContextManager builds it with default limits immediately.
	cm.mu.Lock()
	p0 := cm.Pipeline
	assert.NotNil(t, p0)
	cm.mu.Unlock()

	newLimits := events.Limits{
		MaxHistoryTokens: 9999,
		MaxToolTurns:     50,
		MaxHistoryTurns:  100,
		ContextWindow:    2000,
		TieredThreshold:  1000,
	}

	cm.Reconfigure(newLimits)

	cm.mu.Lock()
	p1 := cm.Pipeline
	cm.mu.Unlock()

	assert.NotNil(t, p1, "Pipeline should be built after Reconfigure")

	// Verify limits were synced (Merged verification)
	h, tool, hist := strategy.getLimits()
	assert.Equal(t, 9999, h)
	assert.Equal(t, 50, tool)
	assert.Equal(t, 100, hist)
	assert.Equal(t, 2000, strategy.getContextWindow())
	assert.Equal(t, 1000, strategy.GetTieredThreshold())

	// Reconfigure again to ensure it updates again (rebuilds pipeline)
	newLimits.MaxHistoryTokens = 8888
	cm.Reconfigure(newLimits)

	cm.mu.Lock()
	p2 := cm.Pipeline
	cm.mu.Unlock()

	assert.NotNil(t, p2)
	assert.NotEqual(t, p1, p2, "Pipeline should be rebuilt on new Reconfigure call")

	h, _, _ = strategy.getLimits()
	assert.Equal(t, 8888, h)
}

func TestContextManager_WindowSize_BoundaryCondition(t *testing.T) {
	counter := &mockTokenCounter{}
	strategy := NewContextStrategy(counter)
	strategy.setContextWindow(10000)

	// totalEntries = 25, numTurns = 5.
	// Initial windowSize = 5 * 4 = 20.
	// The loop will need to increase windowSize.
	contents := make([]*llm.Content, 25)
	for i := 0; i < 25; i++ {
		role := "model"
		if i == 0 {
			role = "user"
		}
		// Second turn starts at the very end to ensure we reach totalEntries
		if i == 24 {
			role = "user"
		}
		contents[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "msg"}}}
	}

	history := &mockHistoryManager{
		contents: contents,
	}

	cm := NewContextManager(strategy, history, nil, nil)
	mockSum := &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "summary", nil, nil
		},
	}
	cm.Summarizer = mockSum

	ctx := context.Background()

	// Requesting 5 turns, but history only has 2 turns.
	// It should reach the end (windowSize = 25), then cap numTurns to totalTurns - 1 = 1.
	msg, _, err := cm.SummarizeRange(ctx, 5, "")
	assert.NoError(t, err)
	assert.Contains(t, msg, "Summarized the first 1 turns")

	// Verify history was updated (summarized 1 turn = 24 messages replaced by 2 summary messages)
	// Original: 25 messages. Turn 1 (24 msgs), Turn 2 (1 msg).
	// After summarization of Turn 1: 2 summary messages + 1 message from Turn 2 = 3 messages.
	assert.Equal(t, 3, history.GetTotalEntries())
}

func TestContextManager_AddContent_ContextCancellation(t *testing.T) {
	cm := NewContextManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail immediately at checkContext
	err := cm.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_SummarizeRange_ContextCancellation(t *testing.T) {
	cm := NewContextManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail immediately at checkContext
	_, _, err := cm.SummarizeRange(ctx, 5, "focus")
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_Prepare_ContextCancellation_PreventsLeak(t *testing.T) {
	t.Parallel()
	strategy := NewContextStrategy(&mockTokenCounter{})
	history := &mockHistoryManager{}
	cm := NewContextManager(strategy, history, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := cm.Prepare(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestContextManager_CheckContext_Cancellation(t *testing.T) {
	cm := NewContextManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	err := cm.checkContext(ctx)
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_Prepare_BoundaryValidation(t *testing.T) {
	strategy := NewContextStrategy(&mockTokenCounter{})

	t.Run("fails on nil message in history", func(t *testing.T) {
		history := &mockHistoryManager{
			contents: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
				nil, // malformed entry
			},
		}
		cm := NewContextManager(strategy, history, nil, nil)
		_, _, err := cm.Prepare(context.Background(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, errInvalidPayload)
		require.Contains(t, err.Error(), "nil message at index 1")
	})

	t.Run("fails on nil part in message", func(t *testing.T) {
		history := &mockHistoryManager{
			contents: []*llm.Content{
				{Role: "user", Parts: []*llm.Part{nil}}, // malformed entry
			},
		}
		cm := NewContextManager(strategy, history, nil, nil)
		_, _, err := cm.Prepare(context.Background(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, errInvalidPayload)
		require.Contains(t, err.Error(), "invalid content at index 0")
	})
}

func TestContextManager_WithLogger(t *testing.T) {
	ctx := context.Background()
	var buf syncWriter
	// Set level to DEBUG to capture the "failed to emit summarization event" log.
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	strategy := NewContextStrategy(&mockTokenCounter{})
	// Add 2 turns to history so that SummarizeRange(ctx, 1, "") can proceed.
	// Summarization requires at least (requestedTurns + 1) turns to preserve the last turn.
	history := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
			{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
		},
	}

	// Use a bus that is shut down to trigger a log in emitSummarizationEvent.
	bus := events.NewSimpleEventBus(ctx, events.WithAsync(false))
	_ = bus.Shutdown(ctx)

	cm := NewContextManager(strategy, history, bus, nil, WithLogger(testLogger))
	cm.Summarizer = &mockSummarizer{
		summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
			return "summary", nil, nil
		},
	}

	// Trigger a condition that causes a log entry.
	// SummarizeRange calls emitSummarizationEvent, which logs a ERROR message if the event bus is closed.
	_, _, _ = cm.SummarizeRange(ctx, 1, "")

	output := buf.String()
	assert.Contains(t, output, `"level":"ERROR"`)
	assert.Contains(t, output, "event_publish_failed")
}
