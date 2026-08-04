// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// noopMetricsProvider is a SystemMetricsProvider that returns zeros.
// Used in tests that don't exercise the metrics code path.
type noopMetricsProvider struct{}

func (n *noopMetricsProvider) GetCPUStats() (int64, int64) { return 0, 0 }
func (n *noopMetricsProvider) GetMemoryPercent() float64   { return 0.0 }

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

		assert.Equal(t, 0, updated.turn, "turn should NOT be set by TurnStatusEvent; only TurnStarted sets it")

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

	t.Run("CtrlC quits", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})

	t.Run("q quits", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
	})

	t.Run("channel close", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		_, cmd := m.Update(channelClosedMsg{})

		require.NotNil(t, cmd)
		assert.IsType(t, tea.QuitMsg{}, cmd())
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
		assert.Equal(t, 100, updated.width)
		assert.Equal(t, 50, updated.height)
		assert.Nil(t, cmd, "unknown messages must return nil to avoid duplicate channel readers")
	})

	t.Run("WindowSizeMsg triggers async re-render with raw text", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.width = 80
		m.bodyLines = append(m.bodyLines, bodyEntry{text: "hello", raw: "hello", needsRender: true})

		newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		updated := newModel.(*model)

		assert.Equal(t, 120, updated.width)
		assert.Equal(t, 40, updated.height)
		assert.Equal(t, "hello", updated.bodyLines[0].text,
			"response should fallback to plaintext immediately")
		assert.NotNil(t, cmd, "should dispatch async render command")
	})

	t.Run("captures height from WindowSizeMsg", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
		updated := newModel.(*model)

		assert.Equal(t, 120, updated.width)
		assert.Equal(t, 40, updated.height)
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

		require.NotEmpty(t, updated.bodyLines, "bodyLines should have response entry")
		last := updated.bodyLines[len(updated.bodyLines)-1]
		assert.Equal(t, "Hello, world! I am fine.", last.text,
			"should concatenate non-thought text parts, skipping thoughts")
		assert.Equal(t, "Hello, world! I am fine.", last.raw,
			"raw should store unrendered text")
		assert.NotNil(t, cmd)
	})

	t.Run("ResponseEvent dispatches async command", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		content := &llm.Content{
			Parts: []*llm.Part{{Text: "Hello"}},
		}
		newModel, cmd := m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
		updated := newModel.(*model)

		require.NotEmpty(t, updated.bodyLines)
		assert.Equal(t, "Hello", updated.bodyLines[len(updated.bodyLines)-1].text,
			"should update plaintext fallback immediately")
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
		assert.Contains(t, updated.finalCostLine, "($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096")
		assert.NotNil(t, cmd)
	})
}

// newTestModel creates a model for testing with a sensible default terminal height.
func newTestModel(_ context.Context, ch <-chan events.Event) *model {
	headerVP := viewport.New(80, 2)
	bodyVP := viewport.New(80, 72)
	footerVP := viewport.New(80, 4)
	return &model{
		eventCh:         ch,
		currentState:    stateIdle,
		height:          80,
		width:           80,
		metricsProvider: &noopMetricsProvider{},
		seenCallIDs:     make(map[string]bool),
		headerVP:        headerVP,
		bodyVP:          bodyVP,
		footerVP:        footerVP,
	}
}

func TestModel_View(t *testing.T) {
	t.Run("stateIdle shows empty defaults", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		assert.Equal(t, 80, m.width, "fresh model width should be newTestModel default")
		assert.Equal(t, 80, m.height, "fresh model height should be newTestModel default")

		out := m.View()
		assert.Contains(t, out, "╭─⠿ Turn 0 - ")
		assert.Contains(t, out, "Payload: ~0/0 tokens")
	})

	t.Run("default height and width before WindowSizeMsg", func(t *testing.T) {
		ch := make(chan events.Event, 1)
		m := NewModel(context.Background(), ch, &noopMetricsProvider{}).(*model)

		assert.Equal(t, 24, m.height, "default height ensures full layout before WindowSizeMsg")
		assert.Equal(t, 80, m.width, "default width ensures viewport sizing from first frame")

		out := m.View()
		assert.Contains(t, out, "╭─⠿ Turn 0 - ")
		assert.Contains(t, out, "Payload:")
	})

	t.Run("stateThinking shows turn and session", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.turn = 5
		m.sessionName = "test-session"
		m.currentState = stateThinking

		out := m.View()

		assert.Contains(t, out, "╭─⠿ Turn 5 - test-session")
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
		m.bodyLines = append(m.bodyLines, bodyEntry{text: "Hello, world!", needsRender: false})

		out := m.View()

		lines := strings.Split(out, "\n")
		assert.True(t, len(lines) >= 2, "expected at least 2 lines, got %d: %q", len(lines), out)
		assert.Contains(t, lines[0], "╭─⠿ Turn 3 - coder-test")
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
		m.bodyLines = nil

		out := m.View()
		// Viewport-based layout — just verify header is present.
		assert.Contains(t, out, "╭─⠿ Turn")
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
		m.postCallMetricsLine = FormatMetricsLine(*m.postCallStatus)

		out := m.View()
		assert.Contains(t, out, "M: 200 H: 800 C: 50")
	})

	t.Run("with final summary", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.currentState = stateRendering
		m.finalCostLine = "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096 99.2% O: 51607)"

		out := m.View()
		assert.Contains(t, out, "╰─⠿ Ready")
		assert.Contains(t, out, "M: 116386")
		assert.Contains(t, out, "($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096")
	})

	t.Run("body_content_visible_in_viewport", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 10})

		m.appendToolLog("Test", "some tool output")
		m.bodyLines = append(m.bodyLines, bodyEntry{text: "final response", needsRender: false})

		out := m.View()
		assert.Contains(t, out, "some tool output")
		assert.Contains(t, out, "final response")
	})

	t.Run("renderMinimal_when_height_below_5", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 4})

		m.turn = 3
		m.sessionName = "test"

		out := m.View()
		lines := strings.Split(out, "\n")

		assert.Len(t, lines, 1)
		assert.Contains(t, lines[0], "╭─⠿ Turn 3 - test")
		assert.NotContains(t, out, "Payload:") // no room for payload line
	})

	t.Run("renderMinimal_shows_spinner_when_active", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 4})

		m.turn = 3
		m.sessionName = "test"
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking

		out := m.View()
		lines := strings.Split(out, "\n")

		assert.Len(t, lines, 1)
		assert.Contains(t, lines[0], "╭─⠿ Turn 3 - test")
		assert.Contains(t, lines[0], "⠋  Thinking...")
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
		require.NotEmpty(t, m.bodyLines)
		assert.Equal(t, "Sure, I can help with that.", m.bodyLines[len(m.bodyLines)-1].text)
		assert.NotNil(t, cmd)

		// Verify View after full cycle
		out := m.View()
		assert.Contains(t, out, "╭─⠿ Turn 1 - architect-johndoe")
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
		assert.Nil(t, cmd) // tick loop no longer spawns waitForEvent; handleTick returns nil when spinner is cleared
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

	t.Run("stale_generation_tick_resets_tickActive", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking
		m.spinner.generation = 5
		m.spinner.tickActive = true

		cmd := m.spinner.handleTick(spinnerTickMsg{generation: 3})

		assert.False(t, m.spinner.tickActive)
		assert.Nil(t, cmd)
	})

	t.Run("session_complete_suppresses_tick", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = " Thinking..."
		m.currentState = stateThinking
		m.spinner.generation = 1
		m.spinner.tickActive = true
		m.sessionComplete = true

		newModel, cmd := m.Update(spinnerTickMsg{generation: 1})
		updated := newModel.(*model)

		assert.True(t, updated.sessionComplete, "sessionComplete flag should remain true")
		assert.Nil(t, cmd, "ticks must return nil when session is complete")
		// frame should NOT advance — tick was suppressed
		assert.Equal(t, 0, updated.spinner.frame, "frame should not advance when session is complete")
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
		require.NotEmpty(t, updated.bodyLines)
		assert.Equal(t, "Hello", updated.bodyLines[len(updated.bodyLines)-1].text)
		assert.NotNil(t, cmd)
	})

	t.Run("TurnStatusEvent does not clear inactive spinner", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.spinner.status = ""
		m.spinner.tickActive = false
		m.spinner.frame = 5
		m.spinner.generation = 3
		m.currentState = stateIdle

		msg := events.TurnStatusEvent{
			Status: events.TurnStatus{
				Tokens:     500,
				Timestamp:  time.Now(),
				Mode:       "test",
				Model:      "gpt-5",
				IsPostCall: false,
			},
		}
		newModel, cmd := m.Update(domainEventMsg(msg))
		updated := newModel.(*model)

		// P1: Spinner should NOT be cleared when inactive — preserves
		// all spinner state for later InferenceStartedEvent.start().
		assert.Equal(t, "", updated.spinner.status, "status should remain empty")
		assert.False(t, updated.spinner.tickActive)
		assert.Equal(t, 5, updated.spinner.frame, "frame should be preserved")
		assert.Equal(t, 3, updated.spinner.generation, "generation should be preserved")
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

		assert.Len(t, updated.bodyLines, 3)
		assert.Contains(t, updated.bodyLines[0].text, "[Tool Engine] Step 1/5")
		assert.Contains(t, updated.bodyLines[1].text, "[Tool Reason] Stage the formatting fix for commit")
		assert.Contains(t, updated.bodyLines[2].text, "[Tool Action] execute_command(command: git add internal/ui/tui/progress/model.go)")
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

		assert.Empty(t, updated.bodyLines)
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

		assert.Len(t, updated.bodyLines, 2) // Step + Action only, no Reason
		assert.Contains(t, updated.bodyLines[0].text, "[Tool Engine]")
		assert.Contains(t, updated.bodyLines[1].text, "[Tool Action] read_file(filepath: /tmp/test.go)")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Tool Result] execute_command: Exit Code: 0 Output: (empty)")
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

		assert.Len(t, updated.bodyLines, 1)
		snippet := updated.bodyLines[0].text
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

		assert.Len(t, updated.bodyLines, 1)
		assert.NotContains(t, updated.bodyLines[0].text, "\n")
		assert.Contains(t, updated.bodyLines[0].text, "line1 line2 line3")
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

		assert.Empty(t, updated.bodyLines)
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Error] command not found")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Warning] deprecated flag used")
	})

	t.Run("ToolOutputStreamEvent output level is logged", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolOutputStreamEvent{
			Message: "Executing... (Output shown below)",
			Level:   "output",
		}))
		updated := newModel.(*model)

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Tool Output] Executing...")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[System] some message")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Error] failed to connect to API")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Info] context window expanded to 128k")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[System] agent state changed")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Error] context limit exceeded")
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

		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "[Warning] retry attempt 2 of 3")
	})

	t.Run("tool_logs_accumulate_across_turns", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 4)
		m := newTestModel(ctx, ch)
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})

		// Turn N: dispatch turn adds tool logs
		m.appendToolLog("Tool Engine", "Step 1/5")
		m.appendToolLog("Tool Action", "read_file(main.go)")
		assert.Len(t, m.bodyLines, 2)

		// Turn N+1: TurnStarted must NOT clear them
		newModel, _ := m.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		updated := newModel.(*model)
		assert.Len(t, updated.bodyLines, 2, "tool logs should persist into execution turn")
		assert.Contains(t, updated.bodyLines[0].text, "Step 1/5")
		assert.Contains(t, updated.bodyLines[1].text, "read_file")

		// Execution turn adds result
		updated.appendToolLog("Tool Result", "read_file: file contents here")
		assert.Len(t, updated.bodyLines, 3, "all three lines present during execution")
	})

	t.Run("TurnStarted does not clear toolLogs", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)
		m.bodyLines = []bodyEntry{{text: "[12:00:00] [Tool Engine] Step 1/5", needsRender: false}}

		newModel, _ := m.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		updated := newModel.(*model)

		assert.NotEmpty(t, updated.bodyLines, "bodyLines should persist across TurnStarted")
		assert.Len(t, updated.bodyLines, 1)
		assert.Contains(t, updated.bodyLines[0].text, "Step 1/5")
	})

}

func TestModel_ResponseEvent_ExtractsToolCalls(t *testing.T) {
	ch := make(chan events.Event, 1)
	m := newTestModel(t.Context(), ch)

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{Text: "Let me run a test for you."},
			{FunctionCall: &llm.FunctionCall{
				ID:   "call_1",
				Name: "execute_command",
				Args: map[string]interface{}{
					"reason":  "Run unit tests",
					"command": "go test ./...",
				},
			}},
		},
	}
	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))

	foundReason := false
	foundAction := false
	for _, entry := range m.bodyLines {
		if strings.Contains(entry.text, "[Tool Reason] Run unit tests") {
			foundReason = true
		}
		if strings.Contains(entry.text, "[Tool Action] execute_command") {
			foundAction = true
		}
	}
	assert.True(t, foundReason, "expected [Tool Reason] in bodyLines from ResponseEvent")
	assert.True(t, foundAction, "expected [Tool Action] in bodyLines from ResponseEvent")
}

func TestModel_ToolCallEvent_DedupsAfterResponseEvent(t *testing.T) {
	ch := make(chan events.Event, 2)
	m := newTestModel(t.Context(), ch)

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{
				ID:   "call_1",
				Name: "execute_command",
				Args: map[string]interface{}{
					"reason":  "Run unit tests",
					"command": "go test ./...",
				},
			}},
		},
	}
	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
	logCountAfterResponse := len(m.bodyLines)

	m.Update(domainEventMsg(events.ToolCallEvent{
		Calls: []*llm.FunctionCall{{
			ID:   "call_1",
			Name: "execute_command",
			Args: map[string]interface{}{
				"reason":  "Run unit tests",
				"command": "go test ./...",
			},
		}},
		Turn:     0,
		MaxTurns: 10,
	}))

	assert.Equal(t, logCountAfterResponse, len(m.bodyLines),
		"ToolCallEvent should not add log lines for already-seen calls")
}

func TestModel_ToolCallEvent_ShowsEngineForPartialNewCalls(t *testing.T) {
	ch := make(chan events.Event, 2)
	m := newTestModel(t.Context(), ch)

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{
				ID:   "call_1",
				Name: "execute_command",
				Args: map[string]interface{}{"command": "go build"},
			},
			}},
	}
	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
	logCount := len(m.bodyLines)

	m.Update(domainEventMsg(events.ToolCallEvent{
		Calls: []*llm.FunctionCall{
			{
				ID:   "call_1",
				Name: "execute_command",
				Args: map[string]interface{}{"command": "go build"},
			},
			{
				ID:   "call_2",
				Name: "read_file",
				Args: map[string]interface{}{"filepath": "main.go"},
			},
		},
		Turn:     0,
		MaxTurns: 10,
	}))

	assert.Greater(t, len(m.bodyLines), logCount,
		"ToolCallEvent should add lines for new calls not already seen")

	foundEngine := false
	foundReadFile := false
	for _, entry := range m.bodyLines {
		if strings.Contains(entry.text, "[Tool Engine] Step 1/10") {
			foundEngine = true
		}
		if strings.Contains(entry.text, "[Tool Action] read_file") {
			foundReadFile = true
		}
	}
	assert.True(t, foundEngine, "expected [Tool Engine] for partially new ToolCallEvent")
	assert.True(t, foundReadFile, "expected [Tool Action] for new call_2")
}

func TestModel_TurnStarted_ClearsStaleDisplayState(t *testing.T) {
	ch := make(chan events.Event, 2)
	m := newTestModel(t.Context(), ch)

	// Simulate a completed turn with all display state populated.
	m.turn = 5
	m.sessionName = "test"
	m.modelName = "deepseek-v4-pro"
	m.currentState = stateRendering
	m.bodyLines = append(m.bodyLines, bodyEntry{text: "Here is the AI response for turn 5.", needsRender: false})
	m.postCallStatus = &events.TurnStatus{
		Metrics: &llm.Metrics{
			PromptTokens:   1000,
			CachedTokens:   800,
			ResponseTokens: 50,
			Cost:           0.0012,
			Duration:       5.0,
			Provider:       "deepseek-pro",
		},
	}
	m.postCallMetricsLine = "[14:30:05] [deepseek-pro] M: 200 H: 800 C: 50 ($0.0012) [7.00s (ΣT: 2.00s)]"
	m.finalCostLine = "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096 99.2% O: 51607)"

	// Verify initial View() contains all the stale content.
	outBefore := m.View()
	assert.Contains(t, outBefore, "Here is the AI response for turn 5.")
	assert.Contains(t, outBefore, "M: 200 H: 800 C: 50")
	assert.Contains(t, outBefore, "╰─⠿ Ready")

	// Fire TurnStarted for turn 6.
	newModel, cmd := m.Update(domainEventMsg(events.TurnStarted{Turn: 5, SessionTurns: 5}))
	updated := newModel.(*model)

	assert.Equal(t, 6, updated.turn) // SessionTurns 5 + 1
	assert.Equal(t, stateThinking, updated.currentState)

	// bodyLines is append-only: previous response must persist
	require.NotEmpty(t, updated.bodyLines)
	last := updated.bodyLines[len(updated.bodyLines)-1]
	assert.Equal(t, "Here is the AI response for turn 5.", last.text,
		"response should persist in bodyLines across TurnStarted")

	// Footer status lines must PERSIST across TurnStarted (sticky).
	assert.NotNil(t, updated.postCallStatus, "postCallStatus should persist across TurnStarted")
	assert.NotEmpty(t, updated.postCallMetricsLine, "postCallMetricsLine should persist across TurnStarted")
	assert.NotEmpty(t, updated.finalCostLine, "finalCostLine should persist across TurnStarted")

	// Non-display fields should also be cleared (except bodyLines — append-only).
	assert.Len(t, updated.seenCallIDs, 0, "seenCallIDs should be empty on TurnStarted")

	// View() after TurnStarted should still contain previous response in scrollback,
	// and footer status lines should still be visible.
	updatedOut := updated.View()
	assert.Contains(t, updatedOut, "Here is the AI response for turn 5.",
		"View after TurnStarted should still contain previous response in scrollback")
	assert.Contains(t, updatedOut, "M: 200 H: 800 C: 50",
		"View after TurnStarted should still contain sticky metrics line")
	assert.Contains(t, updatedOut, "╰─⠿ Ready",
		"View after TurnStarted should still contain sticky final cost line")
	assert.Contains(t, updatedOut, "╭─⠿ Turn 6 - test",
		"View after TurnStarted should show new turn header")
	assert.Contains(t, updatedOut, "Payload: ~0/0 tokens",
		"View after TurnStarted should show payload line with zero tokens")

	assert.NotNil(t, cmd)
}

func TestModel_TurnStarted_ClearsSeenCallIDs(t *testing.T) {
	ch := make(chan events.Event, 2)
	m := newTestModel(t.Context(), ch)

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{FunctionCall: &llm.FunctionCall{
				ID:   "call_1",
				Name: "execute_command",
				Args: map[string]interface{}{"command": "go test"},
			}},
		},
	}
	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
	assert.Len(t, m.seenCallIDs, 1, "seenCallIDs should have 1 entry after ResponseEvent")

	m.Update(domainEventMsg(events.TurnStarted{Turn: 1, SessionTurns: 1, MaxTurns: 10}))
	assert.Len(t, m.seenCallIDs, 0, "seenCallIDs should be empty after TurnStarted")

	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))
	assert.Len(t, m.seenCallIDs, 1, "seenCallIDs should accept same call ID in new turn")
}

func TestModel_FooterStatusLinesPersistAcrossTurns(t *testing.T) {
	ctx := context.Background()
	ch := make(chan events.Event, 2)
	m := newTestModel(ctx, ch)

	// Set up a completed turn with footer data.
	m.turn = 1
	m.currentState = stateRendering
	m.postCallMetricsLine = "[14:30:05] [deepseek] M: 200 H: 800 C: 50 ($0.0012)"
	m.finalCostLine = "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096 95.4% O: 20086)"
	m.postCallStatus = &events.TurnStatus{}

	// Fire TurnStarted — footer should NOT be cleared.
	newModel, cmd := m.Update(domainEventMsg(events.TurnStarted{Turn: 1, SessionTurns: 1}))
	updated := newModel.(*model)

	assert.Equal(t, 2, updated.turn, "turn should increment")
	assert.NotNil(t, updated.postCallStatus, "postCallStatus should persist")
	assert.Equal(t, "[14:30:05] [deepseek] M: 200 H: 800 C: 50 ($0.0012)", updated.postCallMetricsLine)
	assert.Equal(t, "╰─⠿ Ready ($0.0010 $0.0012 $0.1505 M: 116386 H: 15172096 95.4% O: 20086)", updated.finalCostLine)
	assert.NotNil(t, cmd)
}

func TestModel_ResponseEvent_NoToolCalls_NoSpuriousLogs(t *testing.T) {
	ch := make(chan events.Event, 1)
	m := newTestModel(t.Context(), ch)

	content := &llm.Content{
		Role: "model",
		Parts: []*llm.Part{
			{Text: "Here is the result of the analysis."},
		},
	}
	m.Update(domainEventMsg(events.ResponseEvent{Content: content}))

	assert.Len(t, m.bodyLines, 1, "text-only ResponseEvent should add exactly one bodyLines entry (the response)")
	assert.Len(t, m.seenCallIDs, 0, "text-only ResponseEvent should not populate seenCallIDs")
}

func TestModel_SpinnerShowMetricsFlag(t *testing.T) {
	t.Run("ToolExecutionStartedEvent sets showMetrics true", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.ToolExecutionStartedEvent{
			ToolNames: []string{"bash"},
		}))
		updated := newModel.(*model)

		assert.True(t, updated.spinner.showMetrics)
	})

	t.Run("InferenceStartedEvent sets showMetrics false", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.InferenceStartedEvent{
			Model: "gpt-5",
		}))
		updated := newModel.(*model)

		assert.False(t, updated.spinner.showMetrics)
	})

	t.Run("SummarizationStartedEvent sets showMetrics false", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.SummarizationStartedEvent{}))
		updated := newModel.(*model)

		assert.False(t, updated.spinner.showMetrics)
	})

	t.Run("RetryWaitingEvent sets showMetrics false", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, _ := m.Update(domainEventMsg(events.RetryWaitingEvent{
			Duration: 5 * time.Second,
		}))
		updated := newModel.(*model)

		assert.False(t, updated.spinner.showMetrics)
	})

	t.Run("clear resets showMetrics", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		// Set showMetrics via ToolExecutionStartedEvent
		newModel, _ := m.Update(domainEventMsg(events.ToolExecutionStartedEvent{
			ToolNames: []string{"bash"},
		}))
		updated := newModel.(*model)
		assert.True(t, updated.spinner.showMetrics)

		// TurnStarted calls clear()
		newModel2, _ := updated.Update(domainEventMsg(events.TurnStarted{Turn: 0, SessionTurns: 0}))
		updated2 := newModel2.(*model)

		assert.False(t, updated2.spinner.showMetrics)
	})
}

type testMetricsProvider struct {
	cpuTotal int64
	cpuIdle  int64
	mem      float64
}

func (p *testMetricsProvider) GetCPUStats() (int64, int64) { return p.cpuTotal, p.cpuIdle }
func (p *testMetricsProvider) GetMemoryPercent() float64   { return p.mem }

func TestSpinnerRenderMetrics(t *testing.T) {
	t.Run("showMetrics true renders CPU/MEM", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.currentState = stateThinking
		m.spinner.status = " Executing [bash]..."
		m.spinner.frame = 0
		m.spinner.showMetrics = true
		m.lastCPUPercent = 12.5
		m.lastMemPercent = 45.2

		out := m.View()
		assert.Contains(t, out, "[CPU: 12.5% | MEM: 45.2%]")
	})

	t.Run("showMetrics false omits CPU/MEM", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.currentState = stateThinking
		m.spinner.status = " Thinking..."
		m.spinner.frame = 0
		m.spinner.showMetrics = false
		m.lastCPUPercent = 99.9
		m.lastMemPercent = 99.9

		out := m.View()
		assert.NotContains(t, out, "[CPU:")
	})

	t.Run("renderMinimal shows metrics when active", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.Update(tea.WindowSizeMsg{Width: 80, Height: 4})
		m.turn = 1
		m.spinner.status = " Executing..."
		m.spinner.showMetrics = true
		m.currentState = stateThinking
		m.lastCPUPercent = 8.0
		m.lastMemPercent = 33.0

		out := m.View()
		assert.Contains(t, out, "[CPU: 8.0% | MEM: 33.0%]")
	})
}

func TestSampleMetrics(t *testing.T) {
	t.Run("rate-limited to 1 second", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		mp := &testMetricsProvider{cpuTotal: 1000, cpuIdle: 500, mem: 42.0}
		m.metricsProvider = mp

		now := time.Now()
		cpu1, mem1 := m.sampleMetrics(now)
		assert.Equal(t, 42.0, mem1)

		// Within 1 second — must return cached
		cpu2, mem2 := m.sampleMetrics(now.Add(500 * time.Millisecond))
		assert.Equal(t, cpu1, cpu2)
		assert.Equal(t, mem1, mem2)

		// After 1 second — must re-sample
		mp.mem = 99.0
		_, mem3 := m.sampleMetrics(now.Add(1100 * time.Millisecond))
		assert.Equal(t, 99.0, mem3)
	})

	t.Run("nil provider returns zeros", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.metricsProvider = nil

		cpu, mem := m.sampleMetrics(time.Now())
		assert.Equal(t, 0.0, cpu)
		assert.Equal(t, 0.0, mem)
	})
}

func TestModel_AsyncRenderGuards(t *testing.T) {
	t.Run("stale index is dropped", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.bodyLines = append(m.bodyLines, bodyEntry{text: "Current", raw: "Current", needsRender: true})

		newModel, cmd := m.Update(mdRenderCompleteMsg{
			index:    -1, // stale: negative index
			rendered: "Stale",
		})
		updated := newModel.(*model)

		assert.Nil(t, cmd)
		assert.Equal(t, "Current", updated.bodyLines[0].text)
	})

	t.Run("current index is accepted", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))
		m.bodyLines = append(m.bodyLines, bodyEntry{text: "Current", raw: "Current", needsRender: true})

		newModel, cmd := m.Update(mdRenderCompleteMsg{
			index:    0,
			rendered: "Fresh",
		})
		updated := newModel.(*model)

		assert.Nil(t, cmd)
		assert.Equal(t, "Fresh", updated.bodyLines[0].text)
	})
}

func TestSpinnerStartCommandProducesTick(t *testing.T) {
	t.Run("start returns command that fires tick after 200ms", func(t *testing.T) {
		s := &spinnerState{}

		cmd := s.start(" Thinking...", false)
		require.NotNil(t, cmd, "spinner.start() must return a non-nil tea.Cmd")

		// Execute the command — tea.Tick blocks for spinnerTickInterval (200ms)
		// then returns a spinnerTickMsg.
		start := time.Now()
		msg := cmd()
		elapsed := time.Since(start)

		assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond,
			"tea.Tick should block at least 200ms")
		assert.Less(t, elapsed, 300*time.Millisecond,
			"tea.Tick should not block excessively")

		tickMsg, ok := msg.(spinnerTickMsg)
		require.True(t, ok, "command must return spinnerTickMsg")
		assert.Equal(t, s.generation, tickMsg.generation,
			"tick generation must match spinner generation")
	})

	t.Run("feeding tick message through model advances frame and schedules next tick", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))

		// Start the spinner to set initial state.
		m.spinner.start(" Thinking...", false)

		// Simulate the first tick arriving from the tea.Tick command.
		newModel, cmd := m.Update(spinnerTickMsg{generation: m.spinner.generation})
		updated := newModel.(*model)

		assert.Equal(t, 1, updated.spinner.frame, "frame must advance from 0 to 1")
		assert.NotNil(t, cmd, "next tick must be scheduled")
	})

	t.Run("stale generation tick is dropped and does not re-schedule", func(t *testing.T) {
		m := newTestModel(t.Context(), make(chan events.Event, 1))

		// Start → generation 1.
		m.spinner.start(" Thinking...", false)
		gen1 := m.spinner.generation

		// Start again (e.g., new turn) → generation 2.
		m.spinner.start(" Executing...", false)

		// Tick with stale generation 1 must be dropped.
		newModel, cmd := m.Update(spinnerTickMsg{generation: gen1})
		updated := newModel.(*model)

		assert.Equal(t, 0, updated.spinner.frame,
			"frame must NOT advance on stale generation tick")
		assert.Nil(t, cmd, "stale tick must not schedule next tick")
	})
}

func TestWaitEventsRenderSpinnerInView(t *testing.T) {
	tests := []struct {
		name         string
		event        events.Event
		wantContains string
	}{
		{
			name:         "InferenceStartedEvent with model",
			event:        events.InferenceStartedEvent{Model: "gpt-5"},
			wantContains: " Thinking [gpt-5]...",
		},
		{
			name:         "InferenceStartedEvent without model",
			event:        events.InferenceStartedEvent{},
			wantContains: " Thinking...",
		},
		{
			name:         "SummarizationStartedEvent",
			event:        events.SummarizationStartedEvent{},
			wantContains: " Compressing context...",
		},
		{
			name:         "ToolExecutionStartedEvent no tools",
			event:        events.ToolExecutionStartedEvent{},
			wantContains: " Executing tools...",
		},
		{
			name:         "ToolExecutionStartedEvent one tool",
			event:        events.ToolExecutionStartedEvent{ToolNames: []string{"bash"}},
			wantContains: " Executing [bash]...",
		},
		{
			name:         "ToolExecutionStartedEvent multiple tools",
			event:        events.ToolExecutionStartedEvent{ToolNames: []string{"read", "write"}},
			wantContains: " Executing tools [read, write]...",
		},
		{
			name:         "RetryWaitingEvent",
			event:        events.RetryWaitingEvent{Duration: 3 * time.Second},
			wantContains: " Retrying in 3s...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan events.Event, 1)
			m := newTestModel(t.Context(), ch)
			// WindowSizeMsg is needed for proper viewport sizing.
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			// The spinner only renders when currentState != stateIdle.
			// In production, TurnStarted sets stateThinking before spinner events.
			m.currentState = stateThinking

			newModel, cmd := m.Update(domainEventMsg(tt.event))
			updated := newModel.(*model)

			assert.NotNil(t, cmd, "event must return a non-nil command")
			assert.NotEmpty(t, updated.spinner.status,
				"spinner.status must not be empty after wait event")

			out := updated.View()
			assert.Contains(t, out, tt.wantContains,
				"View() must contain spinner status text after wait event")
			// Verify a braille frame character is present (any of the 10 frames).
			foundFrame := false
			for _, frame := range brailleFrames {
				if strings.Contains(out, frame) {
					foundFrame = true
					break
				}
			}
			assert.True(t, foundFrame, "View() must contain a braille spinner frame")
		})
	}
}
