// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGateway is a hand-rolled test double for llm.LLMGateway.
// When GenerateFunc is nil, Generate returns a benign default response.
type mockGateway struct {
	GenerateFunc  func(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error)
	generateCalls int
}

func (m *mockGateway) Generate(ctx context.Context, input []*llm.Content, t []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	m.generateCalls++
	if m.GenerateFunc != nil {
		return m.GenerateFunc(ctx, input, t, resolver)
	}
	return &llm.Content{Role: "model", Parts: []*llm.Part{{Text: "generated"}}}, &llm.Metrics{}, nil
}

// mockEventBus is a hand-rolled test double for events.EventBus.
// When a Func field is nil, the method returns its natural zero value.
type mockEventBus struct {
	PublishFunc     func(ctx context.Context, event events.Event) error
	SubscribeFunc   func(sub func(context.Context, events.Event))
	ShutdownFunc    func(ctx context.Context) error
	FlushFunc       func(ctx context.Context) error
	ListenFunc      func(ctx context.Context) error
	WaitStartedFunc func()

	publishCalls     int
	subscribeCalls   int
	shutdownCalls    int
	flushCalls       int
	listenCalls      int
	waitStartedCalls int
}

func (m *mockEventBus) Publish(ctx context.Context, event events.Event) error {
	m.publishCalls++
	if m.PublishFunc != nil {
		return m.PublishFunc(ctx, event)
	}
	return nil
}

func (m *mockEventBus) Subscribe(sub func(context.Context, events.Event)) {
	m.subscribeCalls++
	if m.SubscribeFunc != nil {
		m.SubscribeFunc(sub)
	}
}

func (m *mockEventBus) Shutdown(ctx context.Context) error {
	m.shutdownCalls++
	if m.ShutdownFunc != nil {
		return m.ShutdownFunc(ctx)
	}
	return nil
}

func (m *mockEventBus) Flush(ctx context.Context) error {
	m.flushCalls++
	if m.FlushFunc != nil {
		return m.FlushFunc(ctx)
	}
	return nil
}

func (m *mockEventBus) Listen(ctx context.Context) error {
	m.listenCalls++
	if m.ListenFunc != nil {
		return m.ListenFunc(ctx)
	}
	return nil
}

func (m *mockEventBus) WaitStarted() {
	m.waitStartedCalls++
	if m.WaitStartedFunc != nil {
		m.WaitStartedFunc()
	}
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

		gw := &mockGateway{}
		bus := &mockEventBus{}
		s := NewSummarizer(gw, bus, WithLogger(testLogger))

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Summary content"}},
		}

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			assert.Equal(t, 4, len(input), "expected 4 history items")
			assert.Equal(t, "Hello", input[0].Parts[0].Text)
			assert.Equal(t, "[Binary Data: image/png]", input[0].Parts[1].Text)
			return respContent, metrics, nil
		}

		var publishCalls []events.Event
		bus.PublishFunc = func(ctx context.Context, event events.Event) error {
			publishCalls = append(publishCalls, event)
			return nil
		}

		summary, m, err := s.Summarize(ctx, subset, "architecture")
		assert.NoError(t, err)
		assert.Equal(t, "Summary content", summary)
		assert.Equal(t, metrics, m)

		output := buf.String()
		assert.Contains(t, output, `"level":"INFO"`)
		assert.Contains(t, output, "Summarization turn completed successfully")

		require.Len(t, publishCalls, 2, "expected 2 publish calls")
		_, ok := publishCalls[0].(events.SummarizationStartedEvent)
		assert.True(t, ok, "first publish should be SummarizationStartedEvent")
		e, ok := publishCalls[1].(events.UsageMetricsEvent)
		require.True(t, ok, "second publish should be UsageMetricsEvent")
		assert.True(t, e.Metrics.IsSummary)
		assert.Equal(t, int32(10), e.Metrics.PromptTokens)

		assert.Equal(t, 1, gw.generateCalls, "expected Generate to be called once")
	})

	t.Run("Event publish failure degrades gracefully", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := &mockGateway{}
		bus := &mockEventBus{}
		s := NewSummarizer(gw, bus, WithLogger(testLogger))

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Summary content"}},
		}

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return respContent, metrics, nil
		}

		bus.PublishFunc = func(ctx context.Context, event events.Event) error {
			return errors.New("simulated bus error")
		}

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

		assert.Equal(t, 1, gw.generateCalls, "expected Generate to be called once")
		assert.Equal(t, 2, bus.publishCalls, "expected Publish to be called twice")
	})

	t.Run("SummarizationStartedEvent publish error logged but does not fail", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := &mockGateway{}
		bus := &mockEventBus{}
		s := NewSummarizer(gw, bus, WithLogger(testLogger))

		metrics := &llm.Metrics{PromptTokens: 10, ResponseTokens: 5}
		respContent := &llm.Content{
			Role:  "model",
			Parts: []*llm.Part{{Text: "Summary content"}},
		}

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return respContent, metrics, nil
		}

		var publishCallCount int
		bus.PublishFunc = func(ctx context.Context, event events.Event) error {
			publishCallCount++
			if publishCallCount == 1 {
				return errors.New("simulated bus error on start")
			}
			return nil
		}

		summary, m, err := s.Summarize(ctx, subset, "architecture")

		assert.NoError(t, err)
		assert.Equal(t, "Summary content", summary)
		assert.Equal(t, metrics, m)

		output := buf.String()
		assert.Contains(t, output, "event_publish_failed")
		assert.Contains(t, output, "simulated bus error on start")
		// Verify event_publish_failed appears only once (only the start event failed)
		assert.Equal(t, 1, countSubstring(output, "event_publish_failed"),
			"expected event_publish_failed exactly once, only for SummarizationStartedEvent")

		assert.Equal(t, 1, gw.generateCalls, "expected Generate to be called once")
		assert.Equal(t, 2, bus.publishCalls, "expected Publish to be called twice")
	})

	t.Run("Empty response", func(t *testing.T) {
		gw := &mockGateway{}
		s := NewSummarizer(gw, nil)

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Parts: []*llm.Part{}}, nil, nil
		}

		_, _, err := s.Summarize(ctx, subset, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})

	t.Run("Transient error", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer
		testLogger := slog.New(slog.NewJSONHandler(&buf, nil))

		gw := &mockGateway{}
		s := NewSummarizer(gw, nil, WithLogger(testLogger))

		transientErr := fmt.Errorf("%w: quota exceeded", llm.ErrTransient)

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, transientErr
		}

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
		gw := &mockGateway{}
		s := NewSummarizer(gw, nil)

		permErr := errors.New("permanent failure")

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, permErr
		}

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "summarization failed permanently")
	})

	t.Run("Nil response content", func(t *testing.T) {
		gw := &mockGateway{}
		s := NewSummarizer(gw, nil)

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return nil, nil, nil
		}

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})

	t.Run("Empty text in response", func(t *testing.T) {
		gw := &mockGateway{}
		s := NewSummarizer(gw, nil)

		gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
			return &llm.Content{Parts: []*llm.Part{{Text: ""}}}, nil, nil
		}

		_, _, err := s.Summarize(ctx, nil, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty content")
	})
}

func TestSummarizer_WithLogger(t *testing.T) {
	ctx := context.Background()
	var buf bytes.Buffer
	testLogger := slog.New(slog.NewJSONHandler(&buf, nil))
	gw := &mockGateway{}
	bus := &mockEventBus{}

	s := NewSummarizer(gw, bus, WithLogger(testLogger))

	bus.PublishFunc = func(ctx context.Context, event events.Event) error {
		return nil
	}

	gw.GenerateFunc = func(ctx context.Context, input []*llm.Content, _ []*tools.ToolDeclaration, _ llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
		return nil, nil, errors.New("simulated logger test error")
	}

	_, _, err := s.Summarize(ctx, nil, "")
	assert.Error(t, err)

	output := buf.String()
	assert.Contains(t, output, `"level":"ERROR"`)
	assert.Contains(t, output, "Summarization turn failed")
	assert.Contains(t, output, "simulated logger test error")

	assert.Equal(t, 1, gw.generateCalls, "expected Generate to be called once")
	assert.Equal(t, 1, bus.publishCalls, "expected Publish to be called once")
}

// countSubstring returns the number of non-overlapping instances of substr in s.
func countSubstring(s, substr string) int {
	return strings.Count(s, substr)
}
