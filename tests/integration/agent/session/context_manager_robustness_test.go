// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/agenttest"
	"github.com/gosharplite/tell-me-go/internal/agent/session"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	domain_llm "github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/testutil"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/history"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
)

func TestContextManager_Prepare_SafetyInjection(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Setup history ending in FunctionResponse
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "call tool"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{FunctionCall: &domain_llm.FunctionCall{Name: "test_tool"}}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{FunctionResponse: &domain_llm.FunctionResponse{Name: "test_tool", Response: map[string]interface{}{"result": "ok"}}}}})

	reg := &agenttest.MockToolRegistry{}
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(reg))
	strategy.SetLimits(1000, 5, 20) // turn 3/5 (remaining 2) -> Triggers warning

	cm := session.NewContextManager(strategy, hManager, bus, nil)

	// Manually set up pipeline for the test as we are bypassing Agent.New()
	cm.Pipeline = session.NewContextPipeline(
		&session.HistoryPruner{
			Policy: &session.SlidingWindowPolicy{MaxTurns: 20},
		},
		&session.TokenGatekeeper{
			MaxTokens:  1000,
			Estimator:  strategy,
			Summarizer: cm.Summarizer,
			Events:     cm.Events,
		},
		&session.WarningInjector{
			Strategy: strategy,
		},
		&session.TransientMerger{},
	)

	// Prepare at turn 3 (approaching limit)
	apiContents, _, err := cm.Prepare(ctx, 3)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify the injected sequence:
	// 0: User "call tool"
	// 1: Model Call
	// 2: User Response + Warning (Appended to TransientParts, merged by transientMerger)

	if len(apiContents) != 3 {
		t.Fatalf("Expected 3 contents after injection, got %d", len(apiContents))
	}

	lastMsg := apiContents[2]
	if lastMsg.Role != "user" || lastMsg.Parts[0].FunctionResponse == nil {
		t.Errorf("Expected User Response at last index, got %v", lastMsg)
	}

	foundWarning := false
	for _, p := range lastMsg.Parts {
		if strings.Contains(p.Text, "URGENT SYSTEM NOTICE") && strings.Contains(p.Text, "Only 2 turns remain") {
			foundWarning = true
			break
		}
	}
	if !foundWarning {
		t.Error("Warning not found in last message parts")
	}
}

func TestContextManager_PerformSummarization_TextOnly(t *testing.T) {
	subset := createTestSubset()

	t.Run("ExecuteSummarize", func(t *testing.T) {
		cm, capturedInput := setupSummarizationTest(t)
		verifyExecuteSummarize(t, cm, subset, capturedInput)
	})

	t.Run("PayloadIntegrity", func(t *testing.T) {
		cm, capturedInput := setupSummarizationTest(t)
		_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")
		verifyPayloadIntegrity(t, capturedInput)
	})

	t.Run("InputTransformation", func(t *testing.T) {
		cm, capturedInput := setupSummarizationTest(t)
		_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")
		verifyInputTransformation(t, capturedInput)
	})

	t.Run("ToolCallMapping", func(t *testing.T) {
		cm, capturedInput := setupSummarizationTest(t)
		_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")
		verifyToolCallMapping(t, capturedInput)
	})

	t.Run("BinaryDataMapping", func(t *testing.T) {
		cm, capturedInput := setupSummarizationTest(t)
		_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")
		verifyBinaryDataMapping(t, capturedInput)
	})
}

func TestContextManager_Prepare_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// 1. Fill history with 10 messages (5 turns)
	for i := 0; i < 5; i++ {
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "user"}}})
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "model"}}})
	}

	bus := testutil.NewCountingEventBus()
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))

	cm := session.NewContextManager(strategy, hManager, bus, nil)

	// Configure pipeline with a pruner that will prune history
	cm.Pipeline = session.NewContextPipeline(
		&session.HistoryPruner{
			Policy: &session.SlidingWindowPolicy{MaxTurns: 2}, // Will keep only last 2 turns (4 messages)
			Events: bus,
		},
	)

	// 2. Call Prepare concurrently
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	errsCh := make(chan error, goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := cm.Prepare(ctx, 1)
			if err != nil {
				errsCh <- err
			}
		}()
	}

	wg.Wait()
	close(errsCh)

	var errs []error
	for err := range errsCh {
		// Concurrent Prepare calls might collide on the persistence step.
		// This is expected behavior with the version-based conflict detection.
		if !domain_llm.IsTransient(err) {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		t.Errorf("Caught %d non-transient errors during concurrent Prepare: %v", len(errs), errs)
	}

	if bus.GetCount() < 1 {
		t.Error("Expected at least one pruning event to be published")
	}
}

func TestContextManager_SummarizeRange_SafetyLimit(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	counter := &agenttest.MockTokenCounter{}
	strategy := session.NewContextStrategy(counter)
	cm := session.NewContextManager(strategy, hManager, nil, nil)

	// Add 4 messages (2 turns)
	for i := 0; i < 2; i++ {
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "msg"}}})
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "msg"}}})
	}

	// Test case: Exactly below threshold (0.9 * contextWindow)
	window := strategy.GetContextWindow()
	counter.SetTokens(int(float64(window) * 0.89))
	ms := &agenttest.MockSummarizer{}
	ms.SetSummarizeFn(func(ctx context.Context, subset []*domain_llm.Content, focus string) (string, *domain_llm.Metrics, error) {
		return "summary", &domain_llm.Metrics{}, nil
	})
	cm.Summarizer = ms

	_, _, err := cm.SummarizeRange(ctx, 1, "")
	if err != nil {
		t.Errorf("expected success below safety limit, got %v", err)
	}

	// Test case: Above threshold
	// Use a fresh manager to ensure we have exactly 2 turns and no interference from previous call
	historyPath2 := filepath.Join(tmpDir, "history2.json")
	hManager2 := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath2, historyPath2+".archive")
	for i := 0; i < 2; i++ {
		_ = hManager2.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "msg"}}})
		_ = hManager2.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "msg"}}})
	}
	cm2 := session.NewContextManager(strategy, hManager2, nil, nil)
	cm2.Summarizer = &agenttest.MockSummarizer{}

	counter.SetTokens(int(float64(window) * 0.91))
	t.Logf("ContextWindow: %d, counter.tokens: %d, safetyLimit: %d", window, int(float64(window)*0.91), int(float64(window)*0.9))
	_, _, err = cm2.SummarizeRange(ctx, 1, "")
	if err == nil {
		t.Errorf("expected safety limit error, got nil")
	} else if !strings.Contains(err.Error(), "exceeds the safety limit") {
		t.Errorf("expected safety limit error, got %v", err)
	}
}

func TestContextManager_Prepare_PersistenceIsolation(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Initial history: 1 turn
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "hello"}}})
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "hi"}}})

	counter := &agenttest.MockTokenCounter{}
	counter.SetTokens(100)
	strategy := session.NewContextStrategy(counter)
	strategy.SetLimits(1000, 10, 20)

	cm := session.NewContextManager(strategy, hManager, nil, nil)

	// Pipeline with warningInjector (Transient)
	cm.Pipeline = session.NewContextPipeline(
		&session.WarningInjector{Strategy: strategy},
		&session.TransientMerger{},
	)

	// Prepare at turn 8 (2 remaining -> triggers warning "Only 2 turns remain")
	apiContents, _, err := cm.Prepare(ctx, 8)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify warning exists in apiContents
	foundWarning := false
	for _, c := range apiContents {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				foundWarning = true
			}
		}
	}
	if !foundWarning {
		t.Error("Expected warning 'Only 2 turns remain' in prepared context")
	}

	// Verify history in manager is NOT changed (it shouldn't have the warning)
	persistedHistory, _ := hManager.GetWindow(ctx, 0, -1)
	for _, c := range persistedHistory {
		for _, p := range c.Parts {
			if strings.Contains(p.Text, "Only 2 turns remain") {
				t.Error("Warning was persisted to history manager, but it should be transient!")
			}
		}
	}
}

func TestContextManager_SummarizeRange_Concurrency(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Initial history: 4 turns (8 messages)
	for i := 0; i < 4; i++ {
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "msg"}}})
		_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "model", Parts: []*domain_llm.Part{{Text: "msg"}}})
	}

	tc := &agenttest.MockTokenCounter{}
	tc.SetTokens(100)
	strategy := session.NewContextStrategy(tc)
	cm := session.NewContextManager(strategy, hManager, nil, nil)

	// Case 1: Safe Concurrent Append
	// We summarize first 2 turns. While summarization is happening, we append turn 5.
	// Since the first 2 turns are unchanged, summarization should succeed.

	summarizeStarted := make(chan struct{})
	summarizeProceed := make(chan struct{})

	ms := &agenttest.MockSummarizer{}
	ms.SetSummarizeFn(func(ctx context.Context, subset []*domain_llm.Content, focus string) (string, *domain_llm.Metrics, error) {
		close(summarizeStarted)
		<-summarizeProceed
		return "Safe Summary", &domain_llm.Metrics{}, nil
	})
	cm.Summarizer = ms

	var wg sync.WaitGroup
	wg.Add(1)
	var summaryErr error
	go func() {
		defer wg.Done()
		_, _, summaryErr = cm.SummarizeRange(ctx, 2, "")
	}()

	<-summarizeStarted
	// Concurrent append
	_ = cm.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "new turn"}}})
	close(summarizeProceed)
	wg.Wait()

	if summaryErr != nil {
		t.Errorf("Expected safe summarization to succeed even with concurrent append, got: %v", summaryErr)
	}

	// Case 2: Unsafe Concurrent Modification
	// We summarize first 2 turns. While happening, we modify turn 1.
	// This should trigger the abort logic.

	summarizeStarted = make(chan struct{})
	summarizeProceed = make(chan struct{})

	ms2 := &agenttest.MockSummarizer{}
	ms2.SetSummarizeFn(func(ctx context.Context, subset []*domain_llm.Content, focus string) (string, *domain_llm.Metrics, error) {
		close(summarizeStarted)
		<-summarizeProceed
		return "Unsafe Summary", &domain_llm.Metrics{}, nil
	})
	cm.Summarizer = ms2

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, summaryErr = cm.SummarizeRange(ctx, 2, "")
	}()

	<-summarizeStarted
	// Concurrent modification of history prefix being summarized
	current, _ := hManager.GetWindow(ctx, 0, -1)
	current[0].Parts[0].Text = "modified"
	_ = hManager.SetContents(ctx, current)
	// We need to trigger a version bump in CM too if it's not watching hManager directly
	// Actually cm.version is internal and only bumped by cm methods.
	// Since we used hManager.SetContents directly, cm.version didn't bump,
	// BUT Content.Equal check in cm.SummarizeRange should still catch it.

	close(summarizeProceed)
	wg.Wait()

	if summaryErr == nil {
		t.Error("Expected summarization to abort when history content changed, but it succeeded")
	} else if !strings.Contains(summaryErr.Error(), "history content changed") {
		t.Errorf("Expected 'history content changed' error, got: %v", summaryErr)
	}
}

func setupSummarizationTest(t *testing.T) (*session.ContextManager, *[]*domain_llm.Content) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.json")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	capturedInput := new([]*domain_llm.Content)
	g := &agenttest.MockGateway{}
	g.SetGenerateFn(func(ctx context.Context, input []*domain_llm.Content, tools []*tools.ToolDeclaration, resolver domain_llm.AssetResolver) (*domain_llm.Content, *domain_llm.Metrics, error) {
		*capturedInput = input
		return &domain_llm.Content{Parts: []*domain_llm.Part{{Text: "Summary"}}}, &domain_llm.Metrics{}, nil
	})
	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	cm := session.NewContextManager(session.NewContextStrategy(session.NewHeuristicTokenCounter(&agenttest.MockToolRegistry{})), hManager, bus, nil)
	cm.Summarizer = llm.NewSummarizer(g, bus)
	return cm, capturedInput
}

func createTestSubset() []*domain_llm.Content {
	return []*domain_llm.Content{
		{
			Role: "model",
			Parts: []*domain_llm.Part{
				{FunctionCall: &domain_llm.FunctionCall{Name: "tool", Args: map[string]interface{}{"a": 1}}},
				{InlineData: &domain_llm.Blob{MIMEType: "image/png", Data: []byte("data")}},
			},
		},
		{
			Role: "user",
			Parts: []*domain_llm.Part{
				{FunctionResponse: &domain_llm.FunctionResponse{Name: "tool", Response: map[string]interface{}{"result": "done"}}},
			},
		},
	}
}

func verifyExecuteSummarize(t *testing.T, cm *session.ContextManager, subset []*domain_llm.Content, capturedInput *[]*domain_llm.Content) {
	_, _, _ = cm.Summarizer.Summarize(context.Background(), subset, "test focus")
	if len(*capturedInput) == 0 {
		t.Fatal("Generate was not called")
	}
}

func verifyPayloadIntegrity(t *testing.T, capturedInput *[]*domain_llm.Content) {
	for i, content := range *capturedInput {
		// Last one is the prompt
		if i == len(*capturedInput)-1 {
			continue
		}
		for _, part := range content.Parts {
			if part.Text == "" {
				t.Errorf("Content %d has empty text part: %+v", i, part)
			}
			if part.FunctionCall != nil || part.FunctionResponse != nil || part.InlineData != nil {
				t.Errorf("Content %d still has structured parts: %+v", i, part)
			}
		}
	}
}

func verifyInputTransformation(t *testing.T, capturedInput *[]*domain_llm.Content) {
	modelTurn := (*capturedInput)[0]
	userTurn := (*capturedInput)[1]

	if !strings.Contains(modelTurn.Parts[0].Text, "[Model called tool") {
		t.Error("Model FunctionCall not converted to text")
	}
	if !strings.Contains(modelTurn.Parts[1].Text, "[Binary Data") {
		t.Error("Model InlineData not converted to text")
	}
	if !strings.Contains(userTurn.Parts[0].Text, "[Tool tool returned") {
		t.Errorf("User FunctionResponse not converted to text, got: %q", userTurn.Parts[0].Text)
	}
}

func verifyToolCallMapping(t *testing.T, capturedInput *[]*domain_llm.Content) {
	modelTurn := (*capturedInput)[0]
	found := false
	for _, p := range modelTurn.Parts {
		if strings.Contains(p.Text, "[Model called tool: tool") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Tool call transformation not found in model turn")
	}
}

func verifyBinaryDataMapping(t *testing.T, capturedInput *[]*domain_llm.Content) {
	modelTurn := (*capturedInput)[0]
	found := false
	for _, p := range modelTurn.Parts {
		if strings.Contains(p.Text, "[Binary Data: image/png]") {
			found = true
			break
		}
	}
	if !found {
		t.Error("Binary data transformation not found in model turn")
	}
}

func TestContextManager_Prepare_ConflictDetection(t *testing.T) {
	tmpDir := t.TempDir()
	historyPath := filepath.Join(tmpDir, "history.jsonl")
	hManager := history.NewManager(persistencetest.NewPlainOSFileSystem(), historyPath, historyPath+".archive")
	ctx := context.Background()

	// Initial message
	_ = hManager.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "initial"}}})

	bus := events.NewSimpleEventBus(context.Background(), events.WithAsync(false))
	events.CleanupBus(t, bus)
	strategy := session.NewContextStrategy(session.NewHeuristicTokenCounter(&agenttest.MockToolRegistry{}))
	cm := session.NewContextManager(strategy, hManager, bus, nil)

	// Custom transformer that blocks mid-execution
	prepareStarted := make(chan struct{})
	prepareResume := make(chan struct{})
	blockingTransformer := &agenttest.MockTransformer{}
	blockingTransformer.SetTransformFn(func(ctx context.Context, req *ports.ContextRequest) error {
		close(prepareStarted)
		<-prepareResume
		return nil
	})

	cm.Pipeline = session.NewContextPipeline(blockingTransformer)

	var wg sync.WaitGroup
	wg.Add(1)
	var prepareErr error
	go func() {
		defer wg.Done()
		// We expect this to fail eventually when it resumes
		_, _, prepareErr = cm.Prepare(ctx, 1)
	}()

	<-prepareStarted
	// Concurrent modification while Prepare is blocked in the pipeline
	_ = cm.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "concurrent"}}})

	close(prepareResume)
	wg.Wait()

	if prepareErr == nil {
		t.Fatal("expected error due to concurrent modification, got nil")
	}
	if !strings.Contains(prepareErr.Error(), "concurrent history modification detected") {
		t.Errorf("expected concurrent modification error, got: %v", prepareErr)
	}
	if !domain_llm.IsTransient(prepareErr) {
		t.Errorf("expected transient error, got: %v", prepareErr)
	}
}

func TestContextManager_GetWindow_Errors(t *testing.T) {
	ctx := context.Background()
	simulatedErr := errors.New("simulated I/O error")

	t.Run("Prepare_Error", func(t *testing.T) {
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetGetWindowErr(simulatedErr)
		cm := session.NewContextManager(nil, hMock, nil, nil)
		_, _, err := cm.Prepare(ctx, 1)
		if !errors.Is(err, simulatedErr) {
			t.Errorf("expected error %v, got %v", simulatedErr, err)
		}
	})

	t.Run("AddContent_Error", func(t *testing.T) {
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*domain_llm.Content{
			{Role: "user", Parts: []*domain_llm.Part{{Text: "test"}}},
		})
		hMock.SetGetWindowErr(simulatedErr)
		cm := session.NewContextManager(nil, hMock, nil, nil)
		err := cm.AddContent(ctx, &domain_llm.Content{Role: "user", Parts: []*domain_llm.Part{{Text: "test"}}})
		if !errors.Is(err, simulatedErr) {
			t.Errorf("expected error %v, got %v", simulatedErr, err)
		}
	})

	t.Run("SummarizeRange_Metadata_Error", func(t *testing.T) {
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*domain_llm.Content{
			{Role: "user", Parts: []*domain_llm.Part{{Text: "msg1"}}},
			{Role: "model", Parts: []*domain_llm.Part{{Text: "msg2"}}},
			{Role: "user", Parts: []*domain_llm.Part{{Text: "msg3"}}},
			{Role: "model", Parts: []*domain_llm.Part{{Text: "msg4"}}},
		})
		hMock.SetGetWindowErr(simulatedErr)
		tc := &agenttest.MockTokenCounter{}
		tc.SetTokens(10)
		strategy := session.NewContextStrategy(tc)
		cm := session.NewContextManager(strategy, hMock, nil, nil)
		cm.Summarizer = &agenttest.MockSummarizer{}

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if !errors.Is(err, simulatedErr) {
			t.Errorf("expected error %v, got %v", simulatedErr, err)
		}
	})

	t.Run("SummarizeRange_Finalize_Error", func(t *testing.T) {
		hMock := &agenttest.MockHistoryManager{}
		hMock.SetInternalContents([]*domain_llm.Content{
			{Role: "user", Parts: []*domain_llm.Part{{Text: "msg1"}}},
			{Role: "model", Parts: []*domain_llm.Part{{Text: "msg2"}}},
			{Role: "user", Parts: []*domain_llm.Part{{Text: "msg3"}}},
			{Role: "model", Parts: []*domain_llm.Part{{Text: "msg4"}}},
		})
		tc := &agenttest.MockTokenCounter{}
		tc.SetTokens(10)
		strategy := session.NewContextStrategy(tc)
		cm := session.NewContextManager(strategy, hMock, nil, nil)
		ms := &agenttest.MockSummarizer{}
		ms.SetSummarizeFn(func(ctx context.Context, subset []*domain_llm.Content, focus string) (string, *domain_llm.Metrics, error) {
			// Set error just before finalizing
			hMock.SetGetWindowErr(simulatedErr)
			return "summary", &domain_llm.Metrics{}, nil
		})
		cm.Summarizer = ms

		_, _, err := cm.SummarizeRange(ctx, 1, "")
		if !errors.Is(err, simulatedErr) {
			t.Errorf("expected error %v, got %v", simulatedErr, err)
		}
	})
}
