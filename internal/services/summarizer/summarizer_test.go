// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package summarizer

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type mockGateway struct {
	mock.Mock
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (<-chan *llm.Content, func() (*llm.Content, *llm.Metrics, error)) {
	args := m.Called(ctx, input, t, resolver)
	ch := args.Get(0)
	if ch == nil {
		return nil, args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
	}
	return ch.(chan *llm.Content), args.Get(1).(func() (*llm.Content, *llm.Metrics, error))
}

func (m *mockGateway) SetSystemInstructions(instr string) {
	m.Called(instr)
}

type mockEventBus struct {
	mock.Mock
}

func (m *mockEventBus) Publish(event events.Event) {
	m.Called(event)
}

func (m *mockEventBus) Subscribe(sub func(events.Event)) {
	m.Called(sub)
}

func TestSummarizer_Summarize(t *testing.T) {
	ctx := context.Background()
	subset := []*llm.Content{
		{
			Role: "user",
			Parts: []*llm.Part{
				{Text: "Hello"},
				{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("fake")}},
			},
		},
		{
			Role: "model",
			Parts: []*llm.Part{
				{FunctionCall: &llm.FunctionCall{Name: "test_tool", Args: map[string]any{"arg1": "val1"}}},
			},
		},
		{
			Role: "tool",
			Parts: []*llm.Part{
				{FunctionResponse: &llm.FunctionResponse{Name: "test_tool", Response: map[string]any{"result": "success"}}},
			},
		},
	}

	t.Run("Success", func(t *testing.T) {
		gw := new(mockGateway)
		bus := new(mockEventBus)
		s := NewSummarizer(gw, bus)

		respCh := make(chan *llm.Content)
		close(respCh)

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role: "model",
			Parts: []*llm.Part{{Text: "Summary content"}},
		}

		gw.On("Generate", ctx, mock.MatchedBy(func(history []*llm.Content) bool {
			if len(history) != 4 {
				return false
			}
			if history[0].Parts[0].Text != "Hello" {
				return false
			}
			if history[0].Parts[1].Text != "[Binary Data: image/png]" {
				return false
			}
			return true
		}), mock.Anything, mock.Anything).Return(respCh, func() (*llm.Content, *llm.Metrics, error) {
			return respContent, metrics, nil
		})

		bus.On("Publish", mock.MatchedBy(func(event events.Event) bool {
			e, ok := event.(events.UsageMetricsEvent)
			return ok && e.Metrics.IsSummary && e.Metrics.PromptTokens == 10
		})).Return()

		summary, m, err := s.Summarize(ctx, subset, "architecture")
		assert.NoError(t, err)
		assert.Equal(t, "Summary content", summary)
		assert.Equal(t, metrics, m)

		gw.AssertExpectations(t)
		bus.AssertExpectations(t)
	})

	t.Run("Empty response", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		respCh := make(chan *llm.Content)
		close(respCh)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(respCh, func() (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Parts: []*llm.Part{}}, nil, nil
		})

		_, _, err := s.Summarize(ctx, subset, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})

	t.Run("Transient error", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		respCh := make(chan *llm.Content)
		close(respCh)

		transientErr := fmt.Errorf("%w: quota exceeded", llm.ErrTransient)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(respCh, func() (*llm.Content, *llm.Metrics, error) {
			return nil, nil, transientErr
		})

		_, _, err := s.Summarize(ctx, subset, "")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, llm.ErrTransient))
	})
}
