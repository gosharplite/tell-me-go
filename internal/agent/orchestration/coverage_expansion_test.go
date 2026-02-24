// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/stretchr/testify/assert"
)

type mockExpTransformer struct {
	priority int
	err      error
}

func (m *mockExpTransformer) Transform(ctx context.Context, req *services.ContextRequest) error {
	return m.err
}

func (m *mockExpTransformer) Priority() int {
	return m.priority
}

func TestExecuteWithPersistence_Comprehensive(t *testing.T) {
	persistErr := errors.New("persist failure")
	transformErr := errors.New("transform failure")

	tests := []struct {
		name        string
		req         *request
		pipeline    []services.ContextTransformer
		persistFn   func(context.Context, []*llm.Content) error
		expectedErr error
	}{
		{
			name: "Error in canonical transformer",
			req:  &request{},
			pipeline: []services.ContextTransformer{
				&mockExpTransformer{priority: 10, err: transformErr},
			},
			expectedErr: transformErr,
		},
		{
			name: "Error in transient transformer",
			req:  &request{},
			pipeline: []services.ContextTransformer{
				&mockExpTransformer{priority: 150, err: transformErr},
			},
			expectedErr: transformErr,
		},
		{
			name: "Error in persistence",
			req:  &request{PersistHistory: true},
			pipeline: []services.ContextTransformer{
				&mockExpTransformer{priority: 10},
				&mockExpTransformer{priority: 150},
			},
			persistFn: func(ctx context.Context, h []*llm.Content) error {
				return persistErr
			},
			expectedErr: persistErr,
		},
		{
			name: "No persist function, no error",
			req:  &request{PersistHistory: true},
			pipeline: []services.ContextTransformer{
				&mockExpTransformer{priority: 10},
			},
			persistFn:   nil,
			expectedErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewContextPipeline(tt.pipeline...)
			err := p.executeWithPersistence(context.Background(), tt.req, tt.persistFn)
			if tt.expectedErr != nil {
				assert.ErrorIs(t, err, tt.expectedErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

type mockExpEventBus struct {
	events []events.Event
}

func (m *mockExpEventBus) Publish(e events.Event) {
	m.events = append(m.events, e)
}
func (m *mockExpEventBus) Subscribe(sub func(events.Event))   {}
func (m *mockExpEventBus) Shutdown(ctx context.Context) error { return nil }
func (m *mockExpEventBus) Flush(ctx context.Context) error    { return nil }

func TestTokenGatekeeper_ValidateHardLimits_Boundaries(t *testing.T) {
	bus := &mockExpEventBus{}
	tg := &tokenGatekeeper{
		MaxTokens: 5000, // Buffer will be 500 (10% of 5000)
		Events:    bus,
	}
	// limit = 5000 - 500 = 4500

	tests := []struct {
		name    string
		tokens  int
		wantErr error
	}{
		{"Exactly at limit", 4500, nil},
		{"One over limit", 4501, llm.ErrContextLimitExceeded},
		{"Well under limit", 100, nil},
		{"Well over limit", 6000, llm.ErrContextLimitExceeded},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bus.events = nil
			err := tg.validateHardLimits(context.Background(), &request{}, tt.tokens)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.NotEmpty(t, bus.events)
			} else {
				assert.NoError(t, err)
				assert.Empty(t, bus.events)
			}
		})
	}

	// MaxTokens <= 0 case
	tg.MaxTokens = 0
	assert.NoError(t, tg.validateHardLimits(context.Background(), &request{}, 9999))
}

func TestTokenGatekeeper_LocateCandidateBlock_EdgeCases(t *testing.T) {
	tg := &tokenGatekeeper{}

	pinnedTurn := []*llm.Content{{Pinned: true}}
	unpinnedTurn := []*llm.Content{{Pinned: false}}

	tests := []struct {
		name          string
		turns         [][]*llm.Content
		target        int
		expectedStart int
		expectedCount int
	}{
		{
			name: "Pinned turn encountered mid-collection resets count",
			turns: [][]*llm.Content{
				unpinnedTurn,
				pinnedTurn,
				unpinnedTurn,
				unpinnedTurn,
				unpinnedTurn,
			},
			target:        3,
			expectedStart: 2,
			expectedCount: 3,
		},
		{
			name: "Pinned turn resets before reaching target, but meets minimum of 2",
			turns: [][]*llm.Content{
				unpinnedTurn,
				unpinnedTurn,
				pinnedTurn,
				unpinnedTurn,
			},
			target:        4,
			expectedStart: 0,
			expectedCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, count := tg.locateCandidateBlock(tt.turns, tt.target)
			assert.Equal(t, tt.expectedStart, start)
			assert.Equal(t, tt.expectedCount, count)
		})
	}
}

func TestCompositePruningPolicy_Name_Coverage(t *testing.T) {
	p := &compositePruningPolicy{}
	assert.Equal(t, "Composite", p.Name())
}

func TestTokenGatekeeper_FindSummarizableRange_ErrorPath(t *testing.T) {
	tg := &tokenGatekeeper{}
	// History with no summarizable blocks (all pinned)
	history := []*llm.Content{
		{Role: "user", Pinned: true},
		{Role: "model", Pinned: true},
	}
	_, _, _, err := tg.findSummarizableRange(history)
	assert.Error(t, err)
}

func TestTokenGatekeeper_HandleSafetyPressure_EdgeCases(t *testing.T) {
	bus := &mockExpEventBus{}
	tg := &tokenGatekeeper{
		MaxTokens: 1000,
		Events:    bus,
	}

	// Case 1: MaxTokens <= 0
	tg.MaxTokens = 0
	tokens, err := tg.handleSafetyPressure(context.Background(), &request{}, 2000)
	assert.NoError(t, err)
	assert.Equal(t, 2000, tokens)

	// Case 2: Tokens under 90%
	tg.MaxTokens = 1000
	tokens, err = tg.handleSafetyPressure(context.Background(), req(), 800)
	assert.NoError(t, err)
	assert.Equal(t, 800, tokens)

	// Case 3: autoSummarize fails but blocked (history too short)
	req := &request{
		History: make([]*llm.Content, 5),
	}
	tokens, err = tg.handleSafetyPressure(context.Background(), req, 950)
	assert.NoError(t, err)
	assert.Equal(t, 950, tokens)
}

func req() *request {
	return &request{}
}
