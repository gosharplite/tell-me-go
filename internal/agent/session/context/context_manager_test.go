// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package context_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	sessctx "github.com/gosharplite/tell-me-go/internal/agent/session/context"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/events/eventstest"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager_PipelineMethods(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	history := &agenttest.MockHistoryManager{}
	factory := &sessctx.Factory{Estimator: strategy}
	cm := sessctx.NewManager(strategy, history, nil, factory)

	// Test SetPipeline
	p := sessctx.NewContextPipeline()
	cm.SetPipeline(p)
	assert.Equal(t, p, cm.Pipeline)
}

func TestManager_GetLimits(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	strategy.SetLimits(1000, 20, 30)
	cm := sessctx.NewManager(strategy, &agenttest.MockHistoryManager{}, nil, nil)

	limits := cm.GetLimits()
	assert.Equal(t, 1000, limits.MaxHistoryTokens)
	assert.Equal(t, 20, limits.MaxToolTurns)
	assert.Equal(t, 30, limits.MaxHistoryTurns)
}

func TestManager_Summarize(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	history := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(strategy, history, nil, nil)

	ctx := context.Background()
	contents := []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "hello"}}}}

	// Case 1: cm.Summarizer is nil.
	summary, metrics, err := cm.Summarize(ctx, contents, "focus")
	assert.NoError(t, err)
	assert.Empty(t, summary)
	assert.Nil(t, metrics)

	// Case 2: cm.Summarizer is present.
	mockSum := &agenttest.MockSummarizer{}
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary result", &llm.Metrics{PromptTokens: 10}, nil
	})
	cm.Summarizer = mockSum
	summary, metrics, err = cm.Summarize(ctx, contents, "focus")
	assert.NoError(t, err)
	assert.Equal(t, "summary result", summary)
	assert.NotNil(t, metrics)
	assert.Equal(t, int32(10), metrics.PromptTokens)
}

func TestManager_SummarizeRange(t *testing.T) {
	counter := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})
	cm := sessctx.NewManager(strategy, history, nil, nil)

	ctx := context.Background()

	// Case 1: Summarizer is nil
	_, _, err := cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Setup Summarizer
	mockSum := &agenttest.MockSummarizer{}
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "range summary", &llm.Metrics{PromptTokens: 5}, nil
	})
	cm.Summarizer = mockSum

	// Case 2: Success
	msg, metrics, err := cm.SummarizeRange(ctx, 1, "focus")
	assert.NoError(t, err)
	assert.Contains(t, msg, "summarized the first 1 turns")
	assert.NotNil(t, metrics)
	assert.Equal(t, int32(5), metrics.PromptTokens)

	// Case 3: History too short
	history.SetInternalContents(nil)
	msg, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.NoError(t, err)
	assert.Equal(t, "history is too short to summarize yet", msg)

	// Case 4: History changed during summarization (shortened)
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		history.SetInternalContents(nil)
		return "late summary", nil, nil
	})
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "history was pruned")

	// Case 5: History content changed during summarization
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		c := history.GetContents()
		c[0] = llm.CloneContent(c[0])
		c[0].Parts[0].Text = "changed"
		history.SetInternalContents(c)
		return "late summary", nil, nil
	})
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "history content changed")

	// Case 6: Safety limit exceeded
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})
	counter.SetTokens(1000)
	strategy.SetContextWindow(500)
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds the safety limit")

	// Reset state
	counter.SetTokens(0)
	strategy.SetContextWindow(1000000)

	// Case 7: Summarizer returns error (Transient)
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "", nil, fmt.Errorf("%w: transient fail", llm.ErrTransient)
	})
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Case 8: Summarizer returns error (Fatal)
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "", nil, fmt.Errorf("fatal fail")
	})
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)

	// Case 9: Event publishing
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	eventstest.CleanupBus(t, bus)

	var logBuf testfixtures.SyncWriter
	testLogger := slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cm.SetLogger(testLogger)
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
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary", nil, nil
	})
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
	history.SetSetContentsErr(fmt.Errorf("persist fail"))
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	history.SetSetContentsErr(nil)

	// Case 11: finalizeSummarization fails (Transient)
	history.SetSetContentsErr(fmt.Errorf("%w: persist fail transient", llm.ErrTransient))
	_, _, err = cm.SummarizeRange(ctx, 1, "")
	assert.Error(t, err)
	history.SetSetContentsErr(nil)
}

func TestManager_Prepare_ClonesContent(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	originalContent := &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "original"}},
	}
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{originalContent})
	cm := sessctx.NewManager(strategy, history, nil, nil)

	ctx := context.Background()
	preparedHistory, _, err := cm.Prepare(ctx, 1)
	assert.NoError(t, err)
	assert.Len(t, preparedHistory, 1)

	// Verify it's a deep copy: modifying original should not affect preparedHistory
	originalContent.Parts[0].Text = "modified"
	assert.NotEqual(t, originalContent.Parts[0].Text, preparedHistory[0].Parts[0].Text)
	assert.Equal(t, "original", preparedHistory[0].Parts[0].Text)
}

func TestManager_Reconfigure_UpdatesPipeline(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	factory := &sessctx.Factory{Estimator: strategy}
	cm := sessctx.NewManager(strategy, &agenttest.MockHistoryManager{}, nil, factory)

	// Initially not nil because NewManager builds it with default limits immediately.
	p0 := cm.Pipeline
	assert.NotNil(t, p0)

	newLimits := events.Limits{
		MaxHistoryTokens: 9999,
		MaxToolTurns:     50,
		MaxHistoryTurns:  100,
		ContextWindow:    2000,
	}

	if err := cm.Reconfigure(newLimits); err != nil {
		t.Fatalf("expected nil error on valid reconfigure, got %v", err)
	}

	p1 := cm.Pipeline
	assert.NotNil(t, p1, "Pipeline should be built after Reconfigure")

	// Verify limits were synced (Merged verification)
	assert.Equal(t, 9999, strategy.GetMaxHistoryTokens())
	assert.Equal(t, 50, strategy.GetMaxToolTurns())
	assert.Equal(t, 2000, strategy.GetContextWindow())

	// Reconfigure again to ensure it updates again (rebuilds pipeline)
	newLimits.MaxHistoryTokens = 8888
	if err := cm.Reconfigure(newLimits); err != nil {
		t.Fatalf("expected nil error on second reconfigure, got %v", err)
	}

	p2 := cm.Pipeline
	assert.NotNil(t, p2)
	assert.NotEqual(t, p1, p2, "Pipeline should be rebuilt on new Reconfigure call")

	assert.Equal(t, 8888, strategy.GetMaxHistoryTokens())
}

func TestManager_Reconfigure_InvalidLimits_RetainsPreviousState(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	strategy.SetLimits(2000, 10, 20) // pre-existing limits
	factory := &sessctx.Factory{Estimator: strategy}
	cm := sessctx.NewManager(strategy, &agenttest.MockHistoryManager{}, nil, factory)

	prevPipeline := cm.Pipeline
	require.NotNil(t, prevPipeline)

	invalidLimits := events.Limits{
		MaxHistoryTokens: -1, // invalid: negative
		MaxToolTurns:     50,
		MaxHistoryTurns:  100,
		ContextWindow:    2000,
	}

	err := cm.Reconfigure(invalidLimits)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "context manager reconfigure")
	assert.Contains(t, err.Error(), "max history tokens")

	// State must be retained — no mutation on validation failure
	assert.Equal(t, prevPipeline, cm.Pipeline, "Pipeline must not be rebuilt on invalid limits")
	assert.Equal(t, 2000, strategy.GetMaxHistoryTokens())
	assert.Equal(t, 10, strategy.GetMaxToolTurns())
}

func TestManager_WindowSize_BoundaryCondition(t *testing.T) {
	counter := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(counter)
	strategy.SetContextWindow(10000)

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

	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents(contents)

	cm := sessctx.NewManager(strategy, history, nil, nil)
	mockSum := &agenttest.MockSummarizer{}
	mockSum.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary", nil, nil
	})
	cm.Summarizer = mockSum

	ctx := context.Background()

	// Requesting 5 turns, but history only has 2 turns.
	// It should reach the end (windowSize = 25), then cap numTurns to totalTurns - 1 = 1.
	msg, _, err := cm.SummarizeRange(ctx, 5, "")
	assert.NoError(t, err)
	assert.Contains(t, msg, "summarized the first 1 turns")

	// Verify history was updated (summarized 1 turn = 24 messages replaced by 2 summary messages)
	// Original: 25 messages. Turn 1 (24 msgs), Turn 2 (1 msg).
	// After summarization of Turn 1: 2 summary messages + 1 message from Turn 2 = 3 messages.
	assert.Equal(t, 3, history.GetTotalEntries())
}

func TestManager_AddContent_ContextCancellation(t *testing.T) {
	cm := sessctx.NewManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail immediately at checkContext
	err := cm.AddContent(ctx, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}})
	require.ErrorIs(t, err, context.Canceled)
}

func TestManager_SummarizeRange_ContextCancellation(t *testing.T) {
	cm := sessctx.NewManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should fail immediately at checkContext
	_, _, err := cm.SummarizeRange(ctx, 5, "focus")
	require.ErrorIs(t, err, context.Canceled)
}

func TestManager_Prepare_ContextCancellation_PreventsLeak(t *testing.T) {
	t.Parallel()
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	history := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(strategy, history, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := cm.Prepare(ctx, 1)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Expected context.Canceled, got %v", err)
	}
}

func TestManager_CheckContext_Cancellation(t *testing.T) {
	_ = sessctx.NewManager(nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// checkContext is unexported. In external test we can't call it.
	// But we can test it indirectly via AddContent or SummarizeRange.
	_ = ctx
}

func TestManager_Prepare_BoundaryValidation(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})

	t.Run("fails on nil message in history", func(t *testing.T) {
		history := &agenttest.MockHistoryManager{}
		history.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			nil, // malformed entry
		})
		cm := sessctx.NewManager(strategy, history, nil, nil)
		_, _, err := cm.Prepare(context.Background(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, sessctx.ErrInvalidPayload)
		require.Contains(t, err.Error(), "nil message at index 1")
	})

	t.Run("fails on nil part in message", func(t *testing.T) {
		history := &agenttest.MockHistoryManager{}
		history.SetInternalContents([]*llm.Content{
			{Role: "user", Parts: []*llm.Part{nil}}, // malformed entry
		})
		cm := sessctx.NewManager(strategy, history, nil, nil)
		_, _, err := cm.Prepare(context.Background(), 1)
		require.Error(t, err)
		require.ErrorIs(t, err, sessctx.ErrInvalidPayload)
		require.Contains(t, err.Error(), "invalid content at index 0")
	})
}

func TestManager_WithLogger(t *testing.T) {
	ctx := context.Background()
	var buf testfixtures.SyncWriter
	// Set level to DEBUG to capture the "skipping summarization event" log.
	testLogger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	// Add 2 turns to history so that SummarizeRange(ctx, 1, "") can proceed.
	// Summarization requires at least (requestedTurns + 1) turns to preserve the last turn.
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "u1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m1"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "u2"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "m2"}}},
	})

	// Use a nil bus to trigger a DEBUG log in emitSummarizationEvent.
	cm := sessctx.NewManager(strategy, history, nil, nil, sessctx.WithLogger(testLogger))
	ms := &agenttest.MockSummarizer{}
	ms.SetSummarizeFn(func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
		return "summary", nil, nil
	})
	cm.Summarizer = ms

	// Trigger a condition that causes a log entry.
	// SummarizeRange calls emitSummarizationEvent, which logs a DEBUG message if the event bus is nil.
	_, _, _ = cm.SummarizeRange(ctx, 1, "")

	output := buf.String()
	assert.Contains(t, output, `"level":"DEBUG"`)
	assert.Contains(t, output, "skipping summarization event")
}

// countingHistoryManager wraps a MockHistoryManager to track GetWindow calls.
type countingHistoryManager struct {
	*agenttest.MockHistoryManager
	mu             sync.Mutex
	getWindowCalls int
}

func (m *countingHistoryManager) GetWindow(ctx context.Context, start, end int) ([]*llm.Content, error) {
	m.mu.Lock()
	m.getWindowCalls++
	m.mu.Unlock()
	return m.MockHistoryManager.GetWindow(ctx, start, end)
}

func (m *countingHistoryManager) GetWindowCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getWindowCalls
}

func TestManager_Prepare_CacheHit(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	baseHistory := &agenttest.MockHistoryManager{}
	baseHistory.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	})
	countingHM := &countingHistoryManager{MockHistoryManager: baseHistory}

	cm := sessctx.NewManager(strategy, countingHM, nil, nil)

	ctx := context.Background()

	// First call — cache miss, loads history and populates cache.
	h1, m1, err := cm.Prepare(ctx, 1)
	require.NoError(t, err)
	require.Len(t, h1, 1)
	require.Equal(t, "hello", h1[0].Parts[0].Text)
	require.Equal(t, 1, countingHM.GetWindowCalls())

	// Second call — cache hit, should NOT call GetWindow again.
	h2, m2, err := cm.Prepare(ctx, 1)
	require.NoError(t, err)
	require.Len(t, h2, 1)
	require.Equal(t, "hello", h2[0].Parts[0].Text)
	// GetWindow should still be 1 — no additional call for cache hit.
	require.Equal(t, 1, countingHM.GetWindowCalls())

	// Verify metadata is present (even if empty) on both calls.
	require.NotNil(t, m1)
	require.NotNil(t, m2)
}

// versionBumpingTransformer is a transient pipeline transformer that bumps the
// Manager's internal version counter between loadHistory and commitToCache,
// causing commitToCache to detect a version mismatch and return ErrTransient.
type versionBumpingTransformer struct {
	cm *sessctx.Manager
}

func (t *versionBumpingTransformer) Priority() int { return 200 } // transient: runs after persistFn

func (t *versionBumpingTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	if err := t.cm.Reconfigure(events.Limits{}); err != nil {
		return err
	}
	return nil
}

// canonicalVersionBumper bumps the Manager's version during the canonical phase
// (Priority < 100), triggering the concurrent modification guard inside
// executePipeline's persistFn closure.
type canonicalVersionBumper struct {
	cm *sessctx.Manager
}

func (t *canonicalVersionBumper) Priority() int { return 50 } // canonical: runs before persistFn

func (t *canonicalVersionBumper) Transform(ctx context.Context, req *ports.ContextRequest) error {
	// Force persistence so persistFn is called.
	req.PersistHistory = true
	if err := t.cm.Reconfigure(events.Limits{}); err != nil {
		return err
	}
	return nil
}

// forcePersistTransformer sets PersistHistory=true so persistFn is invoked.
type forcePersistTransformer struct{}

func (t *forcePersistTransformer) Priority() int { return 10 }

func (t *forcePersistTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	req.PersistHistory = true
	return nil
}

var errTransientFail = errors.New("transient transformer failure")

// failingTransientTransformer returns an error during the transient phase.
// It runs AFTER persistFn (Priority >= 100), testing that a pipeline error
// after successful persistence propagates correctly.
type failingTransientTransformer struct{}

func (t *failingTransientTransformer) Priority() int { return 150 }

func (t *failingTransientTransformer) Transform(ctx context.Context, req *ports.ContextRequest) error {
	return errTransientFail
}

func TestManager_Prepare_CommitToCacheError(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	})

	cm := sessctx.NewManager(strategy, history, nil, nil)

	// Install a pipeline whose transient transformer bumps the version after
	// loadHistory snapshots it but before commitToCache checks it.
	bumper := &versionBumpingTransformer{cm: cm}
	cm.Pipeline = sessctx.NewContextPipeline(bumper)

	ctx := context.Background()
	_, _, err := cm.Prepare(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTransient)
	require.Contains(t, err.Error(), "concurrent history modification detected")
}

func TestManager_Prepare_ExecutePipeline_VersionMismatch(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	})

	cm := sessctx.NewManager(strategy, history, nil, nil)

	// Create a versionBumpingTransformer at canonical tier (Priority < 100)
	// so it executes BEFORE persistFn, triggering the version guard
	// inside executePipeline's closure (manager.go:236-237).
	bumper := &canonicalVersionBumper{cm: cm}
	cm.Pipeline = sessctx.NewContextPipeline(bumper)

	ctx := context.Background()
	_, _, err := cm.Prepare(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTransient)
	require.Contains(t, err.Error(), "concurrent history modification detected during context preparation")
}

func TestManager_Prepare_ExecutePipeline_SetContentsError(t *testing.T) {
	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
	})

	// Inject the I/O failure
	persistErr := fmt.Errorf("%w: disk full during SetContents", llm.ErrTerminal)
	history.SetSetContentsErr(persistErr)

	cm := sessctx.NewManager(strategy, history, nil, nil)

	// Install a canonical transformer that forces persistence
	// (so persistFn is actually called).
	cm.Pipeline = sessctx.NewContextPipeline(&forcePersistTransformer{})

	ctx := context.Background()
	_, _, err := cm.Prepare(ctx, 1)
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTerminal)
	require.ErrorIs(t, err, persistErr)
}

func TestManager_AddContent_SameRole_FastPath(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)

	history := &agenttest.MockHistoryManager{}
	history.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
	})

	cm := sessctx.NewManager(strategy, history, nil, nil)

	newContent := &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "World"}}}

	err := cm.AddContent(context.Background(), newContent)
	require.NoError(t, err)

	contents := history.GetContents()
	require.Len(t, contents, 1)
	assert.Len(t, contents[0].Parts, 2)
	assert.Equal(t, "Hello", contents[0].Parts[0].Text)
	assert.Equal(t, "World", contents[0].Parts[1].Text)
	assert.Equal(t, 1, history.GetTotalEntries())
}

func TestManager_AddContent_FirstMessageMustBeUser(t *testing.T) {
	tests := []struct {
		name    string
		role    string
		wantErr bool
	}{
		{name: "model as first message", role: "model", wantErr: true},
		{name: "system as first message", role: "system", wantErr: true},
		{name: "user as first message", role: "user", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
			history := &agenttest.MockHistoryManager{}
			// Intentionally empty — GetTotalEntries() returns 0
			cm := sessctx.NewManager(strategy, history, nil, nil)

			err := cm.AddContent(context.Background(), &llm.Content{
				Role:  tt.role,
				Parts: []*llm.Part{{Text: "msg"}},
			})

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "first message must be 'user'")
				assert.Contains(t, err.Error(), tt.role)
			} else {
				require.NoError(t, err)
				assert.Equal(t, 1, history.GetTotalEntries())
			}
		})
	}
}

// appendPartsErrorHM wraps MockHistoryManager and overrides AppendParts
// to return a configurable error for testing the AppendParts error path
// in AddContent's fast path.
type appendPartsErrorHM struct {
	*agenttest.MockHistoryManager
	appendPartsErr error
}

func (m *appendPartsErrorHM) AppendParts(ctx context.Context, index int, parts []*llm.Part) error {
	return m.appendPartsErr
}

// emptyWindowHM wraps MockHistoryManager and overrides GetWindow to return
// an empty slice, triggering the len(lastWindow) == 0 fallthrough path in
// AddContent.
type emptyWindowHM struct {
	*agenttest.MockHistoryManager
}

func (m *emptyWindowHM) GetWindow(ctx context.Context, startIdx, endIdx int) ([]*llm.Content, error) {
	return []*llm.Content{}, nil
}

func TestManager_AddContent_SameRole_AppendPartsError(t *testing.T) {
	base := &agenttest.MockHistoryManager{}
	base.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
	})

	sentinelErr := errors.New("append parts failed")
	hm := &appendPartsErrorHM{MockHistoryManager: base, appendPartsErr: sentinelErr}

	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	cm := sessctx.NewManager(strategy, hm, nil, nil)

	err := cm.AddContent(context.Background(), &llm.Content{
		Role:  "user",
		Parts: []*llm.Part{{Text: "World"}},
	})
	require.Error(t, err)
	require.ErrorIs(t, err, sentinelErr)
}

func TestManager_AddContent_EmptyWindow_FallsThroughToAddContent(t *testing.T) {
	base := &agenttest.MockHistoryManager{}
	base.SetInternalContents([]*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "Hello"}}},
	})

	hm := &emptyWindowHM{MockHistoryManager: base}

	strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
	cm := sessctx.NewManager(strategy, hm, nil, nil)

	err := cm.AddContent(context.Background(), &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "World"}},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, base.GetTotalEntries())

	contents := base.GetContents()
	require.Len(t, contents, 2)
	assert.Equal(t, "model", contents[1].Role)
}

func TestManager_Prepare_ExecutePipeline_Coverage(t *testing.T) {
	tests := []struct {
		name           string
		pipeline       []ports.ContextTransformer
		setContentsErr error
		wantErr        error
		verifyPersist  bool // if true, assert SetContents was called before the error
	}{
		{
			name: "happy path: version matches, SetContents succeeds, commitToCache succeeds",
			pipeline: []ports.ContextTransformer{
				&forcePersistTransformer{},
			},
			wantErr: nil,
		},
		{
			name: "transient transformer fails after successful persist",
			pipeline: []ports.ContextTransformer{
				&forcePersistTransformer{},
				&failingTransientTransformer{},
			},
			wantErr:       errTransientFail,
			verifyPersist: true, // SetContents should have been called before transient fail
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			strategy := sessctx.NewStrategy(&agenttest.MockTokenCounter{})
			history := &agenttest.MockHistoryManager{}
			history.SetInternalContents([]*llm.Content{
				{Role: "user", Parts: []*llm.Part{{Text: "hello"}}},
			})
			if tt.setContentsErr != nil {
				history.SetSetContentsErr(tt.setContentsErr)
			}

			// Track whether SetContents was called inside executePipeline's persistFn.
			var setContentsCalled bool
			if tt.verifyPersist {
				history.SetContentsFunc = func(ctx context.Context, contents []*llm.Content) error {
					setContentsCalled = true
					return nil
				}
			}

			cm := sessctx.NewManager(strategy, history, nil, nil)
			cm.Pipeline = sessctx.NewContextPipeline(tt.pipeline...)

			ctx := context.Background()
			h, _, err := cm.Prepare(ctx, 1)

			if tt.wantErr != nil {
				require.Error(t, err)
				require.ErrorIs(t, err, tt.wantErr)
				if tt.verifyPersist {
					require.True(t, setContentsCalled,
						"SetContents should have been called before transient transformer failed")
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, h)
			require.Len(t, h, 1)
		})
	}
}

func TestNewManager_WithFactorySummarizer(t *testing.T) {
	tc := &agenttest.MockTokenCounter{}
	strategy := sessctx.NewStrategy(tc)
	mockSum := &agenttest.MockSummarizer{}
	factory := &sessctx.Factory{
		Estimator:  strategy,
		Summarizer: mockSum,
	}
	history := &agenttest.MockHistoryManager{}
	cm := sessctx.NewManager(strategy, history, nil, factory)

	require.NotNil(t, cm.Summarizer)
	assert.Same(t, mockSum, cm.Summarizer, "Summarizer should be the same pointer as factory.Summarizer")
	assert.NotNil(t, factory.Logger, "factory.Logger should be set to the manager's logger")
}

// stubSessionProvider is a minimal implementation of ports.SessionProvider
// whose methods panic if called. It exists solely to satisfy the type system
// for testing WithSessionProvider.
type stubSessionProvider struct {
	ports.SessionProvider
}

func TestWithSessionProvider(t *testing.T) {
	cm := sessctx.NewManager(nil, nil, nil, nil,
		sessctx.WithSessionProvider(&stubSessionProvider{}),
	)
	assert.NotNil(t, cm.SessionProvider)
}
