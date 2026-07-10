// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package progress

import (
	"context"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_Update(t *testing.T) {
	t.Run("TurnStarted", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(events.TurnStarted{Turn: 20})
		updated := newModel.(*model)

		assert.Equal(t, stateThinking, updated.currentState)
		assert.Equal(t, 20, updated.turn)
		assert.NotNil(t, cmd)
	})

	t.Run("InferenceStartedEvent", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(events.InferenceStartedEvent{Model: "gpt-5"})
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
			},
		}

		newModel, cmd := m.Update(msg)
		updated := newModel.(*model)

		assert.Equal(t, 1500, updated.tokens)
		assert.Equal(t, 32000, updated.maxTokens)
		assert.True(t, updated.timestamp.Equal(ts), "timestamp should be set from Status.Timestamp")
		assert.Equal(t, "deepseek-v4-pro", updated.modelName, "modelName should be set from Status.Model")
		assert.Equal(t, "architect-johndoe", updated.sessionName, "sessionName should be set from Status.Mode")
		assert.Equal(t, stateRendering, updated.currentState)
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

	t.Run("unknown message", func(t *testing.T) {
		ctx := context.Background()
		ch := make(chan events.Event, 1)
		m := newTestModel(ctx, ch)

		newModel, cmd := m.Update(tea.WindowSizeMsg{Width: 100, Height: 50})
		updated := newModel.(*model)

		assert.Equal(t, stateIdle, updated.currentState, "state should be unchanged")
		assert.Equal(t, 0, updated.turn, "turn should be zero value")
		assert.Empty(t, updated.modelName, "modelName should be empty")
		assert.NotNil(t, cmd)
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
