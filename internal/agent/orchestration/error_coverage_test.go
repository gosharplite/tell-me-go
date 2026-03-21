// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/skills"
	"github.com/stretchr/testify/require"
)

func TestContextManager_FindSummarizationBoundary_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel before the loop or during the loop is tricky without more control,
	// but we can at least verify it returns the context error.
	cancel()

	hm := &mockHistoryManager{contents: []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
	}}
	cm := NewContextManager(nil, hm, nil, nil)

	_, _, err := cm.findSummarizationBoundary(ctx, 1, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_ValidateSubset_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cm := NewContextManager(nil, nil, nil, nil)
	subset := make([]*llm.Content, 200) // Need > 100 to trigger the check
	for i := range subset {
		subset[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}
	current := make([]*llm.Content, 200)
	for i := range current {
		current[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "msg"}}}
	}

	err := cm.validateSummarizationSubset(ctx, current, subset)
	require.ErrorIs(t, err, context.Canceled)
}

func TestTokenGatekeeper_AutoSummarize_GroupTurnsError(t *testing.T) {
	tg := &tokenGatekeeper{
		Estimator:  &mockEstimator{},
		Summarizer: &mockSummarizer{},
	}

	// Trigger invalid payload via groupTurns failing
	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "", Parts: []*llm.Part{{Text: "2"}}}, // Empty role
	}
	for i := 0; i < 10; i++ {
		history = append(history, &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "x"}}})
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.autoSummarize(context.Background(), req)
	require.ErrorIs(t, err, errInvalidPayload)
}

type mockFailingEventBus struct {
	err error
}

func (m *mockFailingEventBus) Publish(ctx context.Context, e events.Event) error {
	return m.err
}
func (m *mockFailingEventBus) Subscribe(f func(context.Context, events.Event))     {}
func (m *mockFailingEventBus) Shutdown(ctx context.Context) error { return nil }
func (m *mockFailingEventBus) Flush(ctx context.Context) error    { return nil }

func TestHistoryPruner_EventPublishError(t *testing.T) {
	pruner := &historyPruner{
		Policy: &slidingWindowPolicy{MaxTurns: 1},
		Events: &mockFailingEventBus{err: errors.New("publish failed")},
	}

	history := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "3"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "4"}}},
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	err := pruner.Transform(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, "publish failed", err.Error())
}

type mockFailingSkillSelector struct {
	err error
}

func (m *mockFailingSkillSelector) SelectSkills(ctx context.Context, task string) ([]skills.Skill, error) {
	return nil, m.err
}

func TestSkillInjector_SelectorError(t *testing.T) {
	injector := &skillInjector{
		Selector: &mockFailingSkillSelector{err: errors.New("selector failed")},
	}

	req := &ports.ContextRequest{
		History: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "task"}}},
		},
	}

	err := injector.Transform(context.Background(), req)
	require.NoError(t, err, "Skill injector should swallow selector errors and continue")
}

func TestContextManager_FinalizeSummarization_ArchiveError(t *testing.T) {
	hm := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
		},
	}
	// Use a wrapper to inject Archive error since mockHistoryManager doesn't support it easily
	failingHM := &archiveFailingHM{mockHistoryManager: hm, err: errors.New("archive failed")}

	cm := NewContextManager(nil, failingHM, nil, nil)

	subset := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
	}

	err := cm.finalizeSummarization(context.Background(), subset, 2, "summary")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to archive history")
}

type archiveFailingHM struct {
	*mockHistoryManager
	err error
}

func (m *archiveFailingHM) Archive(ctx context.Context, contents []*llm.Content) error {
	return m.err
}

func TestContextManager_FinalizeSummarization_SetContentsError(t *testing.T) {
	hm := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
			{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
		},
		setContentsErr: errors.New("set contents failed"),
	}

	cm := NewContextManager(nil, hm, nil, nil)

	subset := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
	}

	err := cm.finalizeSummarization(context.Background(), subset, 2, "summary")
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to update history after summarization")
}

func TestContextManager_FinalizeSummarization_PrunedError(t *testing.T) {
	hm := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		},
	}

	cm := NewContextManager(nil, hm, nil, nil)

	subset := []*llm.Content{
		{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "2"}}},
	}

	err := cm.finalizeSummarization(context.Background(), subset, 2, "summary")
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTerminal)
	require.Contains(t, err.Error(), "summarization aborted: history was pruned")
}

func TestTokenGatekeeper_TriggerSummarization_EventError(t *testing.T) {
	mockBus := &mockFailingEventBus{err: errors.New("event error")}
	tg := &tokenGatekeeper{
		Events:    mockBus,
		Estimator: &mockEstimator{},
	}

	req := &ports.ContextRequest{
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.Error(t, err)
	require.Equal(t, "event error", err.Error())
}

func TestTokenGatekeeper_TriggerSummarization_MaintenanceBlocked(t *testing.T) {
	tg := &tokenGatekeeper{
		Estimator: &mockEstimator{},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "", nil, errors.New("summarize error")
			},
		},
	}

	// Not enough history ( < 10)
	history := make([]*llm.Content, 4)
	for i := range history {
		history[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "u"}}}
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.NoError(t, err, "Should swallow error when maintenance is blocked or history too short")
	require.True(t, req.Metadata.MaintenanceBlocked)
}

func TestContextManager_Prepare_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cm := NewContextManager(nil, nil, nil, nil)
	_, _, err := cm.Prepare(ctx, 1)
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_AddContent_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cm := NewContextManager(nil, nil, nil, nil)
	err := cm.AddContent(ctx, &llm.Content{})
	require.ErrorIs(t, err, context.Canceled)
}

func TestContextManager_UpdateCache_VersionMismatch(t *testing.T) {
	cm := NewContextManager(nil, nil, nil, nil)
	cm.version = 2

	req := &request{
		History:  []*llm.Content{},
		Metadata: Metadata{},
	}

	err := cm.updateCache(1, req)
	require.Error(t, err)
	require.ErrorIs(t, err, llm.ErrTransient)
}

func TestContextManager_Prepare_PipelineExecutionError(t *testing.T) {
	hm := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		},
	}

	factory := &PipelineFactory{}
	pipeline := NewContextPipeline(
		&mockTransformer{
			transformFn: func(ctx context.Context, req *ports.ContextRequest) error {
				return errors.New("transform error")
			},
		},
	)

	strategy := NewContextStrategy(&mockTokenCounter{}, nil)
	cm := NewContextManager(strategy, hm, nil, nil)
	cm.Factory = factory
	cm.SetPipeline(pipeline)

	_, _, err := cm.Prepare(context.Background(), 1)
	require.Error(t, err)
	require.Equal(t, "transform error", err.Error())
}

func TestContextManager_AddContent_GetWindowError(t *testing.T) {
	hm := &mockHistoryManager{
		contents: []*llm.Content{
			{Role: "user", Parts: []*llm.Part{{Text: "1"}}},
		},
		getWindowErr: errors.New("get window error"),
	}

	cm := NewContextManager(nil, hm, nil, nil)

	err := cm.AddContent(context.Background(), &llm.Content{Role: "user"})
	require.Error(t, err)
	require.Equal(t, "get window error", err.Error())
}

func TestContextManager_Prepare_HistoryGetWindowError(t *testing.T) {
	hm := &mockHistoryManager{
		getWindowErr: errors.New("get window error"),
	}

	cm := NewContextManager(nil, hm, nil, nil)

	_, _, err := cm.Prepare(context.Background(), 1)
	require.Error(t, err)
	require.Equal(t, "get window error", err.Error())
}

func TestContextManager_EmitSummarizationEvent_Error(t *testing.T) {
	// We want to hit the err != nil branch in emitSummarizationEvent
	mockBus := &mockFailingEventBus{err: errors.New("event error")}
	cm := NewContextManager(nil, nil, mockBus, nil)
	// This function doesn't return the error, so we just call it.
	cm.emitSummarizationEvent(context.Background(), 1, 1)
}

func TestTokenGatekeeper_GetStrategy_Coverage(t *testing.T) {
	// Strategy nil
	tg := &tokenGatekeeper{
		Estimator: &mockEstimator{},
	}
	strategy := tg.getStrategy()
	require.NotNil(t, strategy)

	// Strategy not found
	tg.Strategies = map[string]ThresholdStrategy{"other": nil}
	tg.DefaultTier = "main"
	strategy = tg.getStrategy()
	require.NotNil(t, strategy)
}

func TestTokenGatekeeper_TriggerSummarization_EventError_Swallowed(t *testing.T) {
	// We want to verify that other errors from SafePublish are NOT swallowed.
	mockBus := &mockFailingEventBus{err: errors.New("publish error")}
	tg := &tokenGatekeeper{
		Events:    mockBus,
		Estimator: &mockEstimator{},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", nil, nil
			},
		},
	}

	history := make([]*llm.Content, 20)
	for i := range history {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		history[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "u"}}}
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.Error(t, err, "Event publish error should not be swallowed anymore")
	require.Equal(t, "publish error", err.Error())
}

func TestTokenGatekeeper_LocateCandidateBlock_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tg := &tokenGatekeeper{}
	turns := make([][]*llm.Content, 200)
	for i := range turns {
		turns[i] = []*llm.Content{{Role: "user", Parts: []*llm.Part{{Text: "u"}}}}
	}

	start, num := tg.locateCandidateBlock(ctx, turns, 10)
	require.Equal(t, -1, start)
	require.Equal(t, 0, num)
}

func TestTokenGatekeeper_GetStrategy_Found(t *testing.T) {
	mockStrategy := &dynamicThresholdStrategy{}
	tg := &tokenGatekeeper{
		Strategies:  map[string]ThresholdStrategy{"main": mockStrategy},
		DefaultTier: "main",
	}
	strategy := tg.getStrategy()
	require.Equal(t, mockStrategy, strategy)
}

func TestTokenGatekeeper_TriggerSummarization_AlreadyAttempted(t *testing.T) {
	tg := &tokenGatekeeper{}
	req := &ports.ContextRequest{
		Metadata: ports.ContextMetadata{SummarizationAttempted: true},
	}
	tokens, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.NoError(t, err)
	require.Equal(t, 100, tokens)
}

func TestTokenGatekeeper_TriggerSummarization_OtherError(t *testing.T) {
	tg := &tokenGatekeeper{
		Estimator: &mockEstimator{},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "", nil, errors.New("other error")
			},
		},
	}

	// Enough history to NOT be blocked ( >= 10)
	history := make([]*llm.Content, 10)
	for i := range history {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		history[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "u"}}}
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.Error(t, err)
	require.Equal(t, "other error", err.Error())
}

func TestTokenGatekeeper_TriggerSummarization_NilEvents(t *testing.T) {
	tg := &tokenGatekeeper{
		Estimator: &mockEstimator{},
		Summarizer: &mockSummarizer{
			summarizeFn: func(ctx context.Context, subset []*llm.Content, focus string) (string, *llm.Metrics, error) {
				return "summary", nil, nil
			},
		},
	}

	history := make([]*llm.Content, 20)
	for i := range history {
		role := "user"
		if i%2 != 0 {
			role = "model"
		}
		history[i] = &llm.Content{Role: role, Parts: []*llm.Part{{Text: "u"}}}
	}

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.NoError(t, err)
}

func TestTokenGatekeeper_TriggerSummarization_InvalidPayload(t *testing.T) {
	tg := &tokenGatekeeper{
		Estimator: &mockEstimator{},
	}

	history := make([]*llm.Content, 12)
	for i := range history {
		history[i] = &llm.Content{Role: "user", Parts: []*llm.Part{{Text: "u"}}}
	}
	history[5].Role = "" // Invalid role

	req := &ports.ContextRequest{
		History:  history,
		Metadata: ports.ContextMetadata{},
	}

	_, err := tg.triggerSummarization(context.Background(), req, 100, 10, "test")
	require.ErrorIs(t, err, errInvalidPayload)
}
