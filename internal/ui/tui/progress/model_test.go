// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_Update(t *testing.T) {
	t.Run("TurnStarted", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.TurnStarted{Turn: 20, SessionTurns: 20}))
		updated := newModel.(*model)

		assert.Equal(t, stateThinking, updated.currentState)
		assert.Equal(t, 21, updated.turn) // SessionTurns 20 + 1
		assert.NotNil(t, cmd)
	})

	t.Run("InferenceStartedEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.InferenceStartedEvent{Model: "gpt-5"}))
		updated := newModel.(*model)

		assert.Equal(t, "gpt-5", updated.modelName)
		assert.Equal(t, stateIdle, updated.currentState, "state should be unchanged")
		assert.NotNil(t, cmd)
	})

	t.Run("TurnStatusEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		ts := time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC)
		msg := events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens:           1500,
				MaxHistoryTokens: 32000,
				Timestamp:        ts,
				Mode:             "architect-johndoe",
				Model:            "deepseek-v4-pro",
				SessionTurns:     4,
			},
		}

		newModel, cmd := m.Update(domainEventMsg(msg))
		updated := newModel.(*model)

		assert.Equal(t, 5, updated.turn, "should be SessionTurns + 1")

		assert.Equal(t, 1500, updated.tokens)
		assert.Equal(t, 32000, updated.maxTokens)
		assert.True(t, updated.timestamp.Equal(ts), "timestamp should be set from Status.Timestamp")
		assert.Equal(t, "deepseek-v4-pro", updated.modelName, "modelName should be set from Status.Model")
		assert.Equal(t, "architect-johndoe", updated.sessionName, "sessionName should be set from Status.Mode")
		assert.Equal(t, stateRendering, updated.currentState)
		assert.Nil(t, updated.postCallStatus, "should be nil when IsPostCall is false")
		assert.Empty(t, updated.finalCostLine, "should be empty when IsFinal is false")
		assert.NotNil(t, cmd)
	})

	t.Run("CtrlC", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})

	t.Run("channel close", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		close(ch)
		m := newTestModel(ctx, ch)

		msg := m.waitForEvent()()
		assert.IsType(t, tea.QuitMsg{}, msg)
	})

	t.Run("unknown message returns nil cmd", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
		updated := newModel.(*model)

		assert.Equal(t, stateIdle, updated.currentState, "state should be unchanged")
		assert.Equal(t, 0, updated.turn, "turn should be zero value")
		assert.Empty(t, updated.modelName, "modelName should be empty")
		assert.Nil(t, cmd, "unknown messages must return nil to avoid duplicate channel readers")
	})

	t.Run("WindowSizeMsg triggers re-render with raw text", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.width = 80
		m.rawResponseText = "hello"
		m.mdRender = func(text string, width int) string {
			return fmt.Sprintf("[w=%d]%s", width, text)
		}

		newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		updated := newModel.(*model)

		assert.Equal(t, 120, updated.width)
		assert.Equal(t, "[w=120]hello", updated.responseText,
			"response should be re-rendered with new width")
		assert.Nil(t, cmd)
	})

	t.Run("default message returns nil cmd", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		_, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
		assert.Nil(t, cmd, "non-domain messages must return nil to avoid duplicate channel readers")
	})

	t.Run("error message", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		testErr := fmt.Errorf("test error")
		newModel, cmd := m.Update(testErr)
		updated := newModel.(*model)

		require.NotNil(t, updated.err)
		assert.Equal(t, "test error", updated.err.Error())
		assert.Nil(t, cmd, "internal errors must not trigger channel read")
	})

	t.Run("ResponseEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		content := &llm.Content{
			Parts: []*llm.Part{
				{Text: "Hello, world!"},
				{Text: " How are you?", IsThought: true},
				{Text: " I am fine."},
			},
		}
		newModel, cmd := m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
		updated := newModel.(*model)

		assert.Equal(t, "Hello, world! I am fine.", updated.responseText,
			"should concatenate non-thought text parts, skipping thoughts")
		assert.Equal(t, "Hello, world! I am fine.", updated.rawResponseText,
			"rawResponseText should store unrendered text")
		assert.NotNil(t, cmd)
	})

	t.Run("ResponseEvent with markdown renderer", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.mdRender = func(text string, width int) string {
			return "**" + text + "**"
		}

		content := &llm.Content{
			Parts: []*llm.Part{{Text: "Hello"}},
		}
		newModel, cmd := m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
		updated := newModel.(*model)

		assert.Equal(t, "**Hello**", updated.responseText,
			"should render through mdRender when set")
		assert.NotNil(t, cmd)
	})

	t.Run("TurnStatusEvent with post-call metrics", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		startTime := time.Date(2026, 1, 15, 14, 28, 0, 0, time.UTC)
		metrics := &llm.Metrics{
			PromptTokens:   1000,
			CachedTokens:   800,
			ResponseTokens: 50,
			Cost:           0.0012,
			Duration:       5.0,
			ToolDuration:   2.0,
			Provider:       "deepseek-pro",
		}
		msg := events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens:     1500,
				Timestamp:  time.Now(),
				Mode:       "test",
				Model:      "deepseek-v4-pro",
				IsPostCall: true,
				Metrics:    metrics,
				StartTime:  startTime,
			},
		}

		newModel, cmd := m.Update(domainEventMsg(msg))
		updated := newModel.(*model)

		assert.NotNil(t, updated.postCallStatus)
		assert.Equal(t, int32(1000), updated.postCallStatus.Metrics.PromptTokens)
		assert.NotNil(t, cmd)
	})

	t.Run("TurnStatusEvent with final summary", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		msg := events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens:      1500,
				Timestamp:   time.Now(),
				Mode:        "test",
				Model:       "deepseek-v4-pro",
				IsFinal:     true,
				SessionCost: 0.1505,
				TaskCost:    0.0012,
				TotalM:      116386,
				TotalH:      15172096,
				TotalO:      51607,
				Metrics:     &llm.Metrics{Cost: 0.0010},
			},
		}

		newModel, cmd := m.Update(domainEventMsg(msg))
		updated := newModel.(*model)

		assert.NotEmpty(t, updated.finalCostLine)
		assert.Contains(t, updated.finalCostLine, "Ready")
		assert.Contains(t, updated.finalCostLine, "($0.0010 $0.0012 $0.1505 $0.0000 M: 116386 H: 15172096")
		assert.NotNil(t, cmd)
	})
}

// newTestModel creates a model for testing.
func newTestModel(_ context.Context, ch <-chan events.Event) *model {
	return &model{
		eventCh:      ch,
		currentState: stateIdle,
	}
}

func TestModel_View(t *testing.T) {
	t.Run("stateIdle shows empty defaults", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		out := m.View()

		lines := strings.Split(out, "\n")
		assert.True(t, len(lines) >= 2, "expected at least 2 lines, got %d: %q", len(lines), out)
		assert.Contains(t, lines[0], "╭─ Turn 0 - ")
		assert.Contains(t, lines[1], "Payload: ~0/0 tokens")
	})

	t.Run("stateThinking shows turn and session", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.turn = 5
		m.sessionName = "test-session"
		m.currentState = stateThinking

		out := m.View()

		assert.Contains(t, out, "╭─ Turn 5 - test-session")
	})

	t.Run("stateRendering shows all fields", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.turn = 3
		m.sessionName = "coder-test"
		m.modelName = "gpt-5"
		m.tokens = 5000
		m.maxTokens = 64000
		m.timestamp = time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC)
		m.currentState = stateRendering
		m.responseText = "Hello, world!"

		out := m.View()

		lines := strings.Split(out, "\n")
		assert.True(t, len(lines) >= 2, "expected at least 2 lines, got %d: %q", len(lines), out)
		assert.Contains(t, lines[0], "╭─ Turn 3 - coder-test")
		assert.Contains(t, lines[1], "[14:30:00]")
		assert.Contains(t, lines[1], "~5000/64000")
		assert.Contains(t, lines[1], "coder-test")
		assert.Contains(t, lines[1], "gpt-5")
		assert.Contains(t, out, "Hello, world!")
	})

	t.Run("stateRendering_empty_response_shows_no_extra_line", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateRendering
		m.responseText = ""

		out := m.View()
		lines := strings.Split(out, "\n")
		// header, info, empty trailing — no response line
		assert.Len(t, lines, 3)
	})

	t.Run("with post-call metrics", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateRendering
		m.turn = 1
		m.timestamp = time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC)
		m.postCallStatus = &events.TurnStatus{
			CurrentTurns: 0,
			Metrics: &llm.Metrics{
				PromptTokens:   1000,
				CachedTokens:   800,
				ResponseTokens: 50,
				Cost:           0.0012,
				Duration:       5.0,
				ToolDuration:   2.0,
				Provider:       "deepseek-pro",
			},
			StartTime: time.Date(2026, 1, 15, 14, 28, 0, 0, time.UTC),
		}
		m.postCallMetricsLine = formatMetricsLine(
			m.postCallStatus.Metrics,
			m.postCallStatus.StartTime,
			m.timestamp,
			m.postCallStatus.CurrentTurns+1,
		)

		out := m.View()
		lines := strings.Split(out, "\n")
		// line 0: header, line 1: info, line 2: empty, line 3: token line, line 4: metrics line
		assert.Contains(t, lines[4], "[14:30:00]")
		assert.Contains(t, lines[4], "[deepseek-pro]")
		assert.Contains(t, lines[4], "M: 200 H: 800 C: 50")
	})

	t.Run("with final summary", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateRendering
		m.finalCostLine = "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 $0.0000 M: 116386 H: 15172096 99.2% O: 51607)"

		out := m.View()
		assert.Contains(t, out, "╰─⠿ Ready")
		assert.Contains(t, out, "M: 116386")
		assert.Contains(t, out, "($0.0010 $0.0012 $0.1505 $0.0000 M: 116386 H: 15172096")
	})
}

func TestModel_Integration(t *testing.T) {
	t.Run("idle to thinking to rendering", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 4)
		m := newTestModel(ctx, ch)

		// 1. Start idle
		assert.Equal(t, stateIdle, m.currentState)

		// 2. TurnStarted → thinking
		newModel, cmd := m.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		m = newModel.(*model)
		assert.Equal(t, stateThinking, m.currentState)
		assert.Equal(t, 1, m.turn) // SessionTurns 0 + 1
		assert.NotNil(t, cmd)

		// 3. InferenceStartedEvent → model name set
		newModel, cmd = m.Update(domainEventMsg(events.InferenceStartedEvent{Model: "gpt-5"}))
		m = newModel.(*model)
		assert.Equal(t, "gpt-5", m.modelName)
		assert.NotNil(t, cmd)

		// 4. TurnStatusEvent → rendering with all fields
		ts := time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC)
		newModel, cmd = m.Update(domainEventMsg(events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens:           1500,
				MaxHistoryTokens: 32000,
				Timestamp:        ts,
				Mode:             "architect-johndoe",
				Model:            "deepseek-v4-pro",
				SessionTurns:     0,
			},
		}))
		m = newModel.(*model)
		assert.Equal(t, stateRendering, m.currentState)
		assert.Equal(t, 1, m.turn) // SessionTurns 0 + 1
		assert.Equal(t, 1500, m.tokens)
		assert.Equal(t, "deepseek-v4-pro", m.modelName)
		assert.NotNil(t, cmd)

		// ResponseEvent → store response text
		newModel, cmd = m.Update(domainEventMsg(events.ResponseEvent{
			Content: &llm.Content{
				Parts: []*llm.Part{{Text: "Sure, I can help with that."}},
			},
		}))
		m = newModel.(*model)
		assert.Equal(t, "Sure, I can help with that.", m.responseText)
		assert.NotNil(t, cmd)

		// Verify View after full cycle
		out := m.View()
		assert.Contains(t, out, "╭─ Turn 1 - architect-johndoe")
		assert.Contains(t, out, "deepseek-v4-pro")
		assert.Contains(t, out, "Sure, I can help with that.")
	})

	t.Run("full turn cycle with tool calls", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 8)
		m := newTestModel(ctx, ch)

		// Turn 1: thinking
		newModel, _ := m.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		m = newModel.(*model)
		assert.Equal(t, stateThinking, m.currentState)
		assert.Equal(t, 1, m.turn) // SessionTurns 0 + 1

		// ToolCallEvent — unknown, falls through to default in domainEventMsg switch
		newModel, cmd := m.Update(domainEventMsg(events.ToolCallEvent{
			Calls:    []*llm.FunctionCall{{Name: "read_file"}},
			Turn:     1,
			MaxTurns: 5,
		}))
		m = newModel.(*model)
		assert.Equal(t, stateThinking, m.currentState, "ToolCallEvent should not change state")
		assert.NotNil(t, cmd)

		// ToolResultEvent — also unknown
		newModel, cmd = m.Update(domainEventMsg(events.ToolResultEvent{
			Name:   "read_file",
			Result: tools.ToolResult{Text: "file contents"},
		}))
		m = newModel.(*model)
		assert.Equal(t, stateThinking, m.currentState, "ToolResultEvent should not change state")
		assert.NotNil(t, cmd)

		// TurnStatus → rendering
		ts := time.Now()
		newModel, cmd = m.Update(domainEventMsg(events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens: 2000, MaxHistoryTokens: 64000,
				Timestamp: ts, Mode: "test", Model: "gpt-5",
				SessionTurns: 0,
			},
		}))
		m = newModel.(*model)
		assert.Equal(t, stateRendering, m.currentState)
		assert.Equal(t, 1, m.turn) // SessionTurns 0 + 1
		assert.NotNil(t, cmd)

		// Turn 2: cycle restarts
		newModel, cmd = m.Update(domainEventMsg(events.TurnStarted{Turn: 1, SessionTurns: 1}))
		m = newModel.(*model)
		assert.Equal(t, stateThinking, m.currentState)
		assert.Equal(t, 2, m.turn) // SessionTurns 1 + 1
		assert.NotNil(t, cmd)

		// TurnStatus for Turn 2 — SessionTurns increments
		newModel, cmd = m.Update(domainEventMsg(events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens: 2000, MaxHistoryTokens: 64000,
				Timestamp: time.Now(), Mode: "test", Model: "gpt-5",
				SessionTurns: 1,
			},
		}))
		m = newModel.(*model)
		assert.Equal(t, stateRendering, m.currentState)
		assert.Equal(t, 2, m.turn) // SessionTurns 1 + 1
		assert.NotNil(t, cmd)
	})
}

func TestModel_SpinnerEvents(t *testing.T) {
	t.Run("InferenceStartedEvent without model", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.InferenceStartedEvent{}))
		updated := newModel.(*model)

		assert.Equal(t, " Thinking...", updated.spinner.status)
		assert.Equal(t, 0, updated.spinner.frame)
		assert.NotNil(t, cmd)
	})

	t.Run("InferenceStartedEvent with model", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.InferenceStartedEvent{Model: "gpt-5"}))
		updated := newModel.(*model)

		assert.Equal(t, " Thinking [gpt-5]...", updated.spinner.status)
		assert.NotNil(t, cmd)
	})

	t.Run("SummarizationStartedEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.SummarizationStartedEvent{}))
		updated := newModel.(*model)

		assert.Equal(t, " Compressing context...", updated.spinner.status)
		assert.Equal(t, 0, updated.spinner.frame)
		assert.NotNil(t, cmd)
	})

	t.Run("ToolExecutionStartedEvent no tools", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolExecutionStartedEvent{}))
		updated := newModel.(*model)

		assert.Equal(t, " Executing tools...", updated.spinner.status)
		assert.Equal(t, 0, updated.spinner.frame)
		assert.NotNil(t, cmd)
	})

	t.Run("ToolExecutionStartedEvent one tool", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolExecutionStartedEvent{
			ToolNames: []string{"bash"},
		}))
		updated := newModel.(*model)

		assert.Equal(t, " Executing [bash]...", updated.spinner.status)
		assert.NotNil(t, cmd)
	})

	t.Run("ToolExecutionStartedEvent multiple tools", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolExecutionStartedEvent{
			ToolNames: []string{"read", "write"},
		}))
		updated := newModel.(*model)

		assert.Equal(t, " Executing tools [read, write]...", updated.spinner.status)
		assert.NotNil(t, cmd)
	})

	t.Run("RetryWaitingEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.RetryWaitingEvent{
			Duration: 5 * time.Second,
		}))
		updated := newModel.(*model)

		assert.Equal(t, " Retrying in 5s...", updated.spinner.status)
		assert.Equal(t, 0, updated.spinner.frame)
		assert.NotNil(t, cmd)
	})
}

func TestModel_SpinnerTickAnimation(t *testing.T) {
	t.Run("tick advances frame", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking
		m.spinner.frame = 3

		newModel, cmd := m.Update(spinnerTickMsg{generation: 0})
		updated := newModel.(*model)

		assert.Equal(t, 4, updated.spinner.frame)
		assert.True(t, updated.spinner.tickActive) // re-set by tick() on re-schedule
		assert.NotNil(t, cmd)                      // re-schedules via tick()
	})

	t.Run("tick wraps frame at boundary", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking
		m.spinner.frame = 9 // last frame

		newModel, _ := m.Update(spinnerTickMsg{generation: 0})
		updated := newModel.(*model)

		assert.Equal(t, 0, updated.spinner.frame) // wraps to 0
	})

	t.Run("tick does not re-schedule when spinner cleared", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = "" // cleared
		m.currentState = stateThinking
		m.spinner.tickActive = true

		newModel, cmd := m.Update(spinnerTickMsg{generation: 0})
		updated := newModel.(*model)

		assert.False(t, updated.spinner.tickActive)
		assert.Nil(t, cmd) // tick() returns nil because status is empty
	})

	t.Run("tick does not re-schedule when state is idle", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateIdle // idle
		m.spinner.tickActive = true

		newModel, cmd := m.Update(spinnerTickMsg{generation: 0})
		updated := newModel.(*model)

		// tick() does not check currentState — that guard is in View().
		// The tick fires normally even when the model is idle.
		assert.True(t, updated.spinner.tickActive)
		assert.NotNil(t, cmd)
	})

	t.Run("spinner.tick returns nil when no status", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = ""
		m.currentState = stateThinking

		cmd := m.spinner.tick()
		assert.Nil(t, cmd)
		assert.False(t, m.spinner.tickActive)
	})

	t.Run("spinner.tick returns nil when idle", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateIdle

		// tick() does not check currentState — that guard is in View().
		// The tick fires normally even when the model is idle.
		cmd := m.spinner.tick()
		assert.NotNil(t, cmd)
		assert.True(t, m.spinner.tickActive)
	})

	t.Run("spinner.tick returns command when active", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking

		cmd := m.spinner.tick()
		assert.NotNil(t, cmd)
		assert.True(t, m.spinner.tickActive)
	})
}

func TestModel_SpinnerClearance(t *testing.T) {
	t.Run("TurnStarted clears spinner", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Executing [bash]..."
		m.spinner.tickActive = true
		m.currentState = stateRendering

		newModel, cmd := m.Update(domainEventMsg(events.TurnStarted{Turn: 5, SessionTurns: 5}))
		updated := newModel.(*model)

		assert.Empty(t, updated.spinner.status)
		assert.False(t, updated.spinner.tickActive)
		assert.Equal(t, stateThinking, updated.currentState)
		assert.Equal(t, 6, updated.turn) // SessionTurns + 1
		assert.NotNil(t, cmd)
	})

	t.Run("ResponseEvent clears spinner", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking [gpt-5]..."
		m.spinner.tickActive = true
		m.currentState = stateThinking

		content := &llm.Content{
			Parts: []*llm.Part{{Text: "Hello"}},
		}
		newModel, cmd := m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
		updated := newModel.(*model)

		assert.Empty(t, updated.spinner.status)
		assert.False(t, updated.spinner.tickActive)
		assert.Equal(t, "Hello", updated.responseText)
		assert.NotNil(t, cmd)
	})
}

func TestModel_View_SpinnerLine(t *testing.T) {
	t.Run("renders spinner line with current frame", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.spinner.status = " Executing [bash]..."
		m.spinner.frame = 2 // ⠹

		out := m.View()
		assert.Contains(t, out, "⠹  Executing [bash]...")
	})

	t.Run("renders first frame", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.spinner.status = " Thinking..."
		m.spinner.frame = 0 // ⠋

		out := m.View()
		assert.Contains(t, out, "⠋  Thinking...")
	})

	t.Run("renders last frame", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.spinner.status = " Compressing context..."
		m.spinner.frame = 9 // ⠏

		out := m.View()
		assert.Contains(t, out, "⠏  Compressing context...")
	})

	t.Run("no spinner line in idle state", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateIdle
		m.spinner.status = " Thinking..." // stale
		m.spinner.frame = 0

		out := m.View()
		assert.NotContains(t, out, "⠋")
		assert.NotContains(t, out, "Thinking")
	})

	t.Run("no spinner line when status empty", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.spinner.status = "" // empty

		out := m.View()
		assert.NotContains(t, out, "⠋")
	})

	t.Run("spinner line at end of output", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.turn = 1
		m.sessionName = "test"
		m.spinner.status = " Thinking..."
		m.spinner.frame = 0

		out := m.View()
		lines := strings.Split(out, "\n")
		// Second-to-last line should be the spinner (last line is empty from trailing \n)
		assert.Contains(t, lines[len(lines)-2], "⠋  Thinking...")
	})
}

func TestModel_SpinnerViewAllFrames(t *testing.T) {
	for i, expectedFrame := range brailleFrames {
		t.Run(fmt.Sprintf("frame_%d_%s", i, expectedFrame), func(t *testing.T) {
			ctx := context.Background()
			ch := make(chan events.Event, 1)
			m := newTestModel(ctx, ch)
			m.currentState = stateThinking
			m.spinner.status = " Thinking..."
			m.spinner.frame = i

			out := m.View()
			assert.Contains(t, out, fmt.Sprintf("%s  Thinking...", expectedFrame))
		})
	}
}

func TestModel_ToolLogs(t *testing.T) {
	t.Run("ToolCallEvent renders Step, Reason, and Action", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolCallEvent{
			Turn:     0,
			MaxTurns: 5,
			Calls: []*llm.FunctionCall{
				{
					Name: "execute_command",
					Args: map[string]interface{}{
						"reason":  "Stage the formatting fix for commit",
						"command": "git add internal/ui/tui/progress/model.go",
					},
				},
			},
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 3)
		assert.Contains(t, updated.toolLogs[0], "[Tool Engine] Step 1/5")
		assert.Contains(t, updated.toolLogs[1], "[Tool Reason] Stage the formatting fix for commit")
		assert.Contains(t, updated.toolLogs[2], "[Tool Action] execute_command(command: git add internal/ui/tui/progress/model.go)")
		assert.NotNil(t, cmd)
	})

	t.Run("ToolCallEvent with nil Calls is no-op", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolCallEvent{
			Turn:  0,
			Calls: nil,
		}))
		updated := newModel.(*model)

		assert.Nil(t, updated.toolLogs)
		assert.NotNil(t, cmd)
	})

	t.Run("ToolCallEvent skips empty reason", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolCallEvent{
			Turn:     0,
			MaxTurns: 3,
			Calls: []*llm.FunctionCall{
				{
					Name: "read_file",
					Args: map[string]interface{}{
						"reason":   "",
						"filepath": "/tmp/test.go",
					},
				},
			},
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 2) // Step + Action only, no Reason
		assert.Contains(t, updated.toolLogs[0], "[Tool Engine]")
		assert.Contains(t, updated.toolLogs[1], "[Tool Action] read_file(filepath: /tmp/test.go)")
	})

	t.Run("ToolResultEvent renders snippet", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(domainEventMsg(events.ToolResultEvent{
			Name: "execute_command",
			Result: tools.ToolResult{
				Text: "Exit Code: 0 Output: (empty)",
			},
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Tool Result] execute_command: Exit Code: 0 Output: (empty)")
		assert.NotNil(t, cmd)
	})

	t.Run("ToolResultEvent truncates long text", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		longText := strings.Repeat("x", 250)
		newModel, _ := m.Update(domainEventMsg(events.ToolResultEvent{
			Name: "read_file",
			Result: tools.ToolResult{
				Text: longText,
			},
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		snippet := updated.toolLogs[0]
		// Should be truncated at 200 chars of text: timestamp(11) + tag(14) + "read_file: "(12) + 200 = ~237
		assert.Less(t, len(snippet), 240)
		assert.True(t, strings.HasSuffix(snippet, "..."))
	})

	t.Run("ToolResultEvent collapses newlines", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolResultEvent{
			Name: "read_file",
			Result: tools.ToolResult{
				Text: "line1\nline2\nline3",
			},
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.NotContains(t, updated.toolLogs[0], "\n")
		assert.Contains(t, updated.toolLogs[0], "line1 line2 line3")
	})

	t.Run("ToolResultEvent with empty Name is no-op", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolResultEvent{
			Name: "",
			Result: tools.ToolResult{
				Text: "some text",
			},
		}))
		updated := newModel.(*model)

		assert.Nil(t, updated.toolLogs)
	})

	t.Run("ToolOutputStreamEvent error level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolOutputStreamEvent{
			Message: "command not found",
			Level:   "error",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Error] command not found")
	})

	t.Run("ToolOutputStreamEvent warn level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolOutputStreamEvent{
			Message: "deprecated flag used",
			Level:   "warn",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Warning] deprecated flag used")
	})

	t.Run("ToolOutputStreamEvent output level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolOutputStreamEvent{
			Message: "Executing... (Output shown below)",
			Level:   "output",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Tool Output] Executing...")
	})

	t.Run("ToolOutputStreamEvent unknown level defaults to System", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolOutputStreamEvent{
			Message: "some message",
			Level:   "debug",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[System] some message")
	})

	t.Run("SystemMessageEvent error level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.SystemMessageEvent{
			Message: "failed to connect to API",
			Level:   "error",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Error] failed to connect to API")
	})

	t.Run("SystemMessageEvent info level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.SystemMessageEvent{
			Message: "context window expanded to 128k",
			Level:   "info",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Info] context window expanded to 128k")
	})

	t.Run("SystemMessageEvent default level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.SystemMessageEvent{
			Message: "agent state changed",
			Level:   "debug",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[System] agent state changed")
	})

	t.Run("StatusUpdate error level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.StatusUpdate{
			Message: "context limit exceeded",
			Level:   "error",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Error] context limit exceeded")
	})

	t.Run("StatusUpdate warn level", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.StatusUpdate{
			Message: "retry attempt 2 of 3",
			Level:   "warn",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.toolLogs, 1)
		assert.Contains(t, updated.toolLogs[0], "[Warning] retry attempt 2 of 3")
	})

	t.Run("TurnStarted clears toolLogs", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.toolLogs = []string{"[12:00:00] [Tool Engine] Step 1/5"}

		newModel, _ := m.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		updated := newModel.(*model)

		assert.Nil(t, updated.toolLogs)
	})

	t.Run("View renders toolLogs between header and response", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateThinking
		m.turn = 1
		m.sessionName = "test"
		m.timestamp = time.Date(2026, 1, 15, 14, 30, 0, 0, time.UTC)
		m.toolLogs = []string{
			"[14:30:00] [Tool Engine] Step 1/3",
			"[14:30:00] [Tool Reason] read the file",
		}
		m.responseText = "I'll read that file for you."

		out := m.View()
		lines := strings.Split(out, "\n")

		// Header (line 0), token line (line 1), then tool logs
		assert.Contains(t, lines[0], "╭─ Turn 1 - test")
		assert.Contains(t, lines[2], "[Tool Engine] Step 1/3")
		assert.Contains(t, lines[3], "[Tool Reason] read the file")
		// Response text after tool logs
		assert.Contains(t, out, "I'll read that file for you.")
	})
}
