// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	args := m.Called(ctx, input, t, resolver)
	var content *llm.Content
	if args.Get(0) != nil {
		content = args.Get(0).(*llm.Content)
	}
	var metrics *llm.Metrics
	if args.Get(1) != nil {
		metrics = args.Get(1).(*llm.Metrics)
	}
	return content, metrics, args.Error(2)
}

type mockEventBus struct {
	mock.Mock
}

func (m *mockEventBus) Publish(ctx context.Context, event events.Event) error {
	args := m.Called(ctx, event)
	return args.Error(0)
}

func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {
	m.Called(sub)
}

func (m *mockEventBus) Shutdown(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *mockEventBus) Flush(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
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
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := new(mockGateway)
		bus := new(mockEventBus)
		s := NewSummarizer(gw, bus, WithLogger(testLogger))

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role:  "model",
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
		}), mock.Anything, mock.Anything).Return(respContent, metrics, nil)

		bus.On("Publish", mock.Anything, mock.MatchedBy(func(event events.Event) bool {
			_, ok := event.(events.SummarizationStartedEvent)
			return ok
		})).Return(nil)

		bus.On("Publish", mock.Anything, mock.MatchedBy(func(event events.Event) bool {
			e, ok := event.(events.UsageMetricsEvent)
			return ok && e.Metrics.IsSummary && e.Metrics.PromptTokens == 10
		})).Return(nil)

		summary, m, err := s.Summarize(ctx, subset, "architecture")
		assert.NoError(t, err)
		assert.Equal(t, "Summary content", summary)
		assert.Equal(t, metrics, m)

		output := buf.String()
		assert.Contains(t, output, `"level":"INFO"`)
		assert.Contains(t, output, "Summarization turn completed successfully")

		gw.AssertExpectations(t)
		bus.AssertExpectations(t)
	})

	t.Run("Event publish failure degrades gracefully", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := new(mockGateway)
		bus := new(mockEventBus)
		s := NewSummarizer(gw, bus, WithLogger(testLogger))

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Summary content"}},
		}

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(respContent, metrics, nil)

		// Simulate event bus failures
		bus.On("Publish", mock.Anything, mock.MatchedBy(func(event events.Event) bool {
			_, ok := event.(events.SummarizationStartedEvent)
			return ok
		})).Return(errors.New("simulated bus error"))

		bus.On("Publish", mock.Anything, mock.MatchedBy(func(event events.Event) bool {
			_, ok := event.(events.UsageMetricsEvent)
			return ok
		})).Return(errors.New("simulated bus error"))

		// Execute Summarization
		summary, m, err := s.Summarize(ctx, subset, "architecture")

		// Assert: Summarization STILL succeeds despite publish errors
		assert.NoError(t, err)
		assert.Equal(t, "Summary content", summary)
		assert.Equal(t, metrics, m)

		// Assert: The errors were properly captured by the logger
		output := buf.String()
		assert.Contains(t, output, "event_publish_failed")
		assert.Contains(t, output, "simulated bus error")

		gw.AssertExpectations(t)
		bus.AssertExpectations(t)
	})

	t.Run("Empty response", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Parts: []*llm.Part{}}, nil, nil)

		_, _, err := s.Summarize(ctx, subset, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})

	t.Run("Transient error", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := new(mockGateway)
		s := NewSummarizer(gw, nil, WithLogger(testLogger))

		transientErr := fmt.Errorf("%w: quota exceeded", llm.ErrTransient)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, transientErr)

		_, _, err := s.Summarize(ctx, subset, "")
		assert.Error(t, err)
		assert.True(t, errors.Is(err, llm.ErrTransient))

		output := buf.String()
		assert.Contains(t, output, `"level":"ERROR"`)
		assert.Contains(t, output, "Summarization turn failed")
		assert.Contains(t, output, "quota exceeded")
	})
}

func TestSummarizer_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("Permanent error", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		permErr := errors.New("permanent failure")

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, permErr)

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "summarization failed permanently")
	})

	t.Run("Nil response content", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, nil)

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})

	t.Run("Empty text in response", func(t *testing.T) {
		gw := new(mockGateway)
		s := NewSummarizer(gw, nil)

		gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(&llm.Content{Parts: []*llm.Part{{Text: ""}}}, nil, nil)

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})
}

func TestSummarizer_WithLogger(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	gw := new(mockGateway)
	bus := new(mockEventBus)

	s := NewSummarizer(gw, bus, WithLogger(testLogger))

	bus.On("Publish", mock.Anything, mock.MatchedBy(func(event events.Event) bool {
		_, ok := event.(events.SummarizationStartedEvent)
		return ok
	})).Return(nil)

	gw.On("Generate", ctx, mock.Anything, mock.Anything, mock.Anything).Return(nil, nil, errors.New("simulated logger test error"))

	_, _, err := s.Summarize(ctx, nil, "")
	assert.Error(t, err)

	output := buf.String()
	assert.Contains(t, output, `"level":"ERROR"`)
	assert.Contains(t, output, "Summarization turn failed")
	assert.Contains(t, output, "simulated logger test error")
}
