// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type mockUIRenderer struct {
	mu                     sync.Mutex
	wg                     sync.WaitGroup
	renderResponseCalled   bool
	lastRenderContent      *llm.Content
	streamResponseCalled   bool
	logUsageCalled         bool
	lastMetrics            *llm.Metrics
	logTurnStatusCalled    bool
	lastTurnStatus         events.TurnStatus
	logToolCallCalled      bool
	lastToolCalls          []*llm.FunctionCall
	logToolResultCalled    bool
	lastToolName           string
	lastToolResult         tools.ToolResult
	logSystemMessageCalled bool
	lastSystemMessage      string
	lastSystemLevel        string
	receivedContent        []*llm.Content
	skipConsumer           bool
}

func (m *mockUIRenderer) RenderResponse(respContent *llm.Content, showThoughts, rawOutput bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.renderResponseCalled = true
	m.lastRenderContent = respContent
}

func (m *mockUIRenderer) StreamResponse(ctx stdctx.Context, showThoughts, rawOutput bool) (chan<- *llm.Content, func() *llm.Content) {
	m.mu.Lock()
	m.streamResponseCalled = true
	skip := m.skipConsumer
	m.mu.Unlock()

	ch := make(chan *llm.Content)
	if !skip {
		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			for c := range ch {
				m.mu.Lock()
				m.receivedContent = append(m.receivedContent, c)
				m.mu.Unlock()
			}
		}()
	}
	return ch, func() *llm.Content {
		if !skip {
			close(ch)
			m.wg.Wait()
		}
		return &llm.Content{}
	}
}

func (m *mockUIRenderer) LogTurnStatus(status events.TurnStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logTurnStatusCalled = true
	m.lastTurnStatus = status
}

func (m *mockUIRenderer) LogUsage(ctx stdctx.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logUsageCalled = true
	m.lastMetrics = metrics
}

func (m *mockUIRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logToolCallCalled = true
	m.lastToolCalls = calls
}

func (m *mockUIRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logToolResultCalled = true
	m.lastToolName = name
	m.lastToolResult = result
}

func (m *mockUIRenderer) LogSystemMessage(msg string, level string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logSystemMessageCalled = true
	m.lastSystemMessage = msg
	m.lastSystemLevel = level
}

func (m *mockUIRenderer) RenderStatus(ctx stdctx.Context, status events.TurnStatus) {
	m.LogTurnStatus(status)
}

func (m *mockUIRenderer) RenderEvent(ctx stdctx.Context, event events.Event) {}

func (m *mockUIRenderer) SetUseColor(use bool) {}

func TestUISubscriber_HandleEvent_NilContext(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := newUISubscriber(renderer, false, false, false, false, "")

	// This should not panic and should log a warning
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

		renderer.mu.Lock()
		defer renderer.mu.Unlock()
		if !renderer.logSystemMessageCalled {
			t.Error("LogSystemMessage was not called for nil context in ResponseStreamEvent")
		} else if renderer.lastSystemLevel != "warn" {
			t.Errorf("Expected warn level, got %s", renderer.lastSystemLevel)
		}
	})

	t.Run("UsageMetricsEvent with nil context", func(t *testing.T) {
		renderer.mu.Lock()
		renderer.logSystemMessageCalled = false
		renderer.lastMetrics = nil
		renderer.mu.Unlock()

		metrics := &llm.Metrics{TotalTokens: 100}
		s.HandleEvent(events.UsageMetricsEvent{
			Context: nil,
			Metrics: metrics,
		})

		renderer.mu.Lock()
		defer renderer.mu.Unlock()
		if !renderer.logUsageCalled {
			t.Error("LogUsage was not called")
		} else if renderer.lastMetrics != metrics {
			t.Error("LogUsage called with wrong metrics")
		}

		if !renderer.logSystemMessageCalled {
			t.Error("LogSystemMessage was not called for nil context in UsageMetricsEvent")
		} else if renderer.lastSystemLevel != "warn" {
			t.Errorf("Expected warn level, got %s", renderer.lastSystemLevel)
		}
	})
}

func TestUISubscriber_HandleEvent_OtherEvents(t *testing.T) {
	tests := []struct {
		name  string
		event events.Event
		check func(*testing.T, *mockUIRenderer)
	}{
		{
			name:  "TurnStatusEvent",
			event: events.TurnStatusEvent{Status: events.TurnStatus{CurrentTurns: 1}},
			check: checkTurnStatus,
		},
		{
			name:  "ToolCallEvent",
			event: events.ToolCallEvent{Calls: []*llm.FunctionCall{{Name: "test"}}},
			check: checkToolCall,
		},
		{
			name:  "ToolResultEvent",
			event: events.ToolResultEvent{Name: "test", Result: tools.ToolResult{Text: "ok"}},
			check: checkToolResult,
		},
		{
			name:  "SystemMessageEvent",
			event: events.SystemMessageEvent{Message: "msg", Level: "info"},
			check: checkSystemMessage,
		},
		{
			name:  "StatusUpdate",
			event: events.StatusUpdate{Message: "msg", Level: "info"},
			check: checkStatusUpdate,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			renderer := &mockUIRenderer{}
			s := newUISubscriber(renderer, true, true, false, false, "")

			s.HandleEvent(tt.event)

			renderer.mu.Lock()
			defer renderer.mu.Unlock()
			tt.check(t, renderer)
		})
	}
}

func checkTurnStatus(t *testing.T, m *mockUIRenderer) {
	t.Helper()
	if !m.logTurnStatusCalled {
		t.Error("LogTurnStatus was not called")
	}
	if m.lastTurnStatus.CurrentTurns != 1 {
		t.Errorf("Expected turn 1, got %d", m.lastTurnStatus.CurrentTurns)
	}
}

func checkToolCall(t *testing.T, m *mockUIRenderer) {
	t.Helper()
	if !m.logToolCallCalled {
		t.Error("LogToolCall was not called")
	}
	if len(m.lastToolCalls) != 1 || m.lastToolCalls[0].Name != "test" {
		t.Errorf("Wrong tool calls logged: %+v", m.lastToolCalls)
	}
}

func checkToolResult(t *testing.T, m *mockUIRenderer) {
	t.Helper()
	if !m.logToolResultCalled {
		t.Error("LogToolResult was not called")
	}
	if m.lastToolName != "test" || m.lastToolResult.Text != "ok" {
		t.Errorf("Wrong tool result logged: %s, %v", m.lastToolName, m.lastToolResult)
	}
}

func checkSystemMessage(t *testing.T, m *mockUIRenderer) {
	t.Helper()
	if !m.logSystemMessageCalled {
		t.Error("LogSystemMessage was not called")
	}
	if m.lastSystemMessage != "msg" || m.lastSystemLevel != "info" {
		t.Errorf("Wrong system message logged: %s, %s", m.lastSystemMessage, m.lastSystemLevel)
	}
}

func checkStatusUpdate(t *testing.T, m *mockUIRenderer) {
	t.Helper()
	if !m.logSystemMessageCalled {
		t.Error("LogSystemMessage was not called by StatusUpdate")
	}
	if m.lastSystemMessage != "msg" || m.lastSystemLevel != "info" {
		t.Errorf("Wrong status update logged: %s, %s", m.lastSystemMessage, m.lastSystemLevel)
	}
}

func TestUISubscriber_HandleEvent_Cancellation(t *testing.T) {
	renderer := &mockUIRenderer{}
	s := newUISubscriber(renderer, false, false, false, false, "")

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
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
	s := newUISubscriber(renderer, false, false, false, false, "")

	streamCh := make(chan *llm.Content, 2)
	content1 := &llm.Content{Parts: []*llm.Part{{Text: "hello"}}}
	content2 := &llm.Content{Parts: []*llm.Part{{Text: " world"}}}

	streamCh <- content1
	streamCh <- content2
	close(streamCh)

	s.HandleEvent(events.ResponseStreamEvent{
		Context: stdctx.Background(),
		Stream:  streamCh,
	})

	renderer.mu.Lock()
	defer renderer.mu.Unlock()
	if len(renderer.receivedContent) != 2 {
		t.Errorf("Expected 2 content chunks, got %d", len(renderer.receivedContent))
	} else {
		if renderer.receivedContent[0] != content1 {
			t.Error("First content chunk mismatch")
		}
		if renderer.receivedContent[1] != content2 {
			t.Error("Second content chunk mismatch")
		}
	}
}

func TestUISubscriber_HandleEvent_CancellationDuringBlock(t *testing.T) {
	renderer := &mockUIRenderer{skipConsumer: true}
	s := newUISubscriber(renderer, false, false, false, false, "")

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	streamCh := make(chan *llm.Content)

	done := make(chan struct{})
	go func() {
		s.HandleEvent(events.ResponseStreamEvent{
			Context: ctx,
			Stream:  streamCh,
		})
		close(done)
	}()

	// Send one item. It should block in HandleEvent because skipConsumer is true.
	content := &llm.Content{Parts: []*llm.Part{{Text: "blocking"}}}

	select {
	case streamCh <- content:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timed out sending content to streamCh")
	}

	// Now it should be blocked at `uiCh <- c` in HandleEvent.
	// We cancel the stdctx.
	cancel()

	select {
	case <-done:
		// Success: HandleEvent returned despite being blocked on channel send.
	case <-time.After(1 * time.Second):
		t.Error("HandleEvent did not terminate on context cancellation while blocked on send")
	}
}
