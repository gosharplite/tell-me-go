// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockUIRenderer struct {
	streamResponseCalled   bool
	logUsageCalled         bool
	logTurnStatusCalled    bool
	logToolCallCalled      bool
	logToolResultCalled    bool
	logSystemMessageCalled bool
	receivedContent        []*llm.Content
}

func (m *mockUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {}
func (m *mockUIRenderer) StreamResponse(ctx context.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	m.streamResponseCalled = true
	ch := make(chan *llm.Content, 100)
	go func() {
		for c := range ch {
			m.receivedContent = append(m.receivedContent, c)
		}
	}()
	return ch, func() *llm.Content { return &llm.Content{} }
}
func (m *mockUIRenderer) LogTurnStatus(status events.TurnStatus) { m.logTurnStatusCalled = true }
func (m *mockUIRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.logUsageCalled = true
}
func (m *mockUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.logToolCallCalled = true
}
func (m *mockUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	m.logToolResultCalled = true
}
func (m *mockUIRenderer) LogSystemMessage(msg string, level string) { m.logSystemMessageCalled = true }

func TestUISubscriber_HandleEvent_NilContext(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := NewUISubscriber(renderer, false, false, false, "")

	// This should not panic
	t.Run("ResponseStreamEvent with nil context", func(t *testing.T) {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("HandleEvent panicked with nil context: %v", r)
			}
		}()

		streamCh := make(chan *llm.Content)
		close(streamCh)
		s.HandleEvent(events.ResponseStreamEvent{
			Context: nil,
			Stream:  streamCh,
		})
	})

	t.Run("UsageMetricsEvent with nil context", func(t *testing.T) {
		s.HandleEvent(events.UsageMetricsEvent{
			Context: nil,
			Metrics: &llm.Metrics{},
		})
		if !renderer.logUsageCalled {
			t.Error("LogUsage was not called")
		}
	})
}

func TestUISubscriber_HandleEvent_OtherEvents(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := NewUISubscriber(renderer, true, true, false, "")

	t.Run("TurnStatusEvent", func(t *testing.T) {
		s.HandleEvent(events.TurnStatusEvent{Status: events.TurnStatus{}})
		if !renderer.logTurnStatusCalled {
			t.Error("LogTurnStatus was not called")
		}
	})

	t.Run("ToolCallEvent", func(t *testing.T) {
		s.HandleEvent(events.ToolCallEvent{Calls: []*llm.FunctionCall{{Name: "test"}}})
		if !renderer.logToolCallCalled {
			t.Error("LogToolCall was not called")
		}
	})

	t.Run("ToolResultEvent", func(t *testing.T) {
		s.HandleEvent(events.ToolResultEvent{Name: "test", Result: tools.ToolResult{Text: "ok"}})
		if !renderer.logToolResultCalled {
			t.Error("LogToolResult was not called")
		}
	})

	t.Run("SystemMessageEvent", func(t *testing.T) {
		s.HandleEvent(events.SystemMessageEvent{Message: "msg", Level: "info"})
		if !renderer.logSystemMessageCalled {
			t.Error("LogSystemMessage was not called")
		}
	})

	t.Run("StatusUpdate", func(t *testing.T) {
		renderer.logSystemMessageCalled = false
		s.HandleEvent(events.StatusUpdate{Message: "msg", Level: "info"})
		if !renderer.logSystemMessageCalled {
			t.Error("LogSystemMessage was not called by StatusUpdate")
		}
	})
}

func TestUISubscriber_HandleEvent_Cancellation(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := NewUISubscriber(renderer, false, false, false, "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	streamCh := make(chan *llm.Content)
	// We don't close streamCh to test if it breaks the loop via context

	done := make(chan struct{})
	go func() {
		s.HandleEvent(events.ResponseStreamEvent{
			Context: ctx,
			Stream:  streamCh,
		})
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Error("HandleEvent did not terminate on context cancellation")
	}
}

func TestUISubscriber_HandleEvent_StreamDataFlow(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := NewUISubscriber(renderer, false, false, false, "")

	streamCh := make(chan *llm.Content, 2)
	content1 := &llm.Content{Parts: []*llm.Part{{Text: "hello"}}}
	content2 := &llm.Content{Parts: []*llm.Part{{Text: " world"}}}
	
	streamCh <- content1
	streamCh <- content2
	close(streamCh)

	s.HandleEvent(events.ResponseStreamEvent{
		Context: context.Background(),
		Stream:  streamCh,
	})

	// Wait a bit for the background drain goroutine in the mock to finish
	time.Sleep(10 * time.Millisecond)

	if len(renderer.receivedContent) != 2 {
		t.Errorf("Expected 2 content chunks, got %d", len(renderer.receivedContent))
	}
}
