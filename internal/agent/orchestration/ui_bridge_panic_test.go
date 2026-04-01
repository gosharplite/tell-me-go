// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"context"
	inframock "github.com/gosharplite/tell-me-go/internal/infrastructure/testing"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type panicMockRenderer struct {
	shouldPanic bool
	lastMsg     string
	lastLevel   string
}

func (m *panicMockRenderer) StartSpinner(ctx context.Context) func() { return func() {} }
func (m *panicMockRenderer) StartSpinnerWithStatus(ctx context.Context, status string) func() {
	if m.shouldPanic {
		panic("simulated ui panic")
	}
	return func() {}
}
func (m *panicMockRenderer) StartSpinnerWithMetrics(ctx context.Context, status string) func() {
	return func() {}
}
func (m *panicMockRenderer) RenderResponse(content *llm.Content, showThoughts, rawOutput bool) {}
func (m *panicMockRenderer) LogTurnStatus(status events.TurnStatus)                            {}
func (m *panicMockRenderer) LogUsage(ctx context.Context, metrics *llm.Metrics, logFile string, startTime time.Time) {
}
func (m *panicMockRenderer) LogToolCall(calls []*llm.FunctionCall, turn, maxTurns int, showTools bool) {
}
func (m *panicMockRenderer) LogToolResult(name string, result tools.ToolResult, showTools bool) {}
func (m *panicMockRenderer) LogSystemMessage(msg string, level string) {
	m.lastMsg = msg
	m.lastLevel = level
}
func (m *panicMockRenderer) SetUseColor(use bool)       {}
func (m *panicMockRenderer) SetForceSpinner(force bool) {}

func TestUIBridge_PanicResilience(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	mock := &panicMockRenderer{}

	// Synchronous bus for deterministic testing
	bus := events.NewSimpleEventBus(ctx, events.WithWorkers(0))
	inframock.CleanupBus(t, bus)

	bridge := newUIBridge(ctx, mock, true, true, false, true, "test.log")
	bus.Subscribe(bridge.handleEvent)

	// Phase 1: The Panic
	mock.shouldPanic = true
	err := bus.Publish(ctx, events.InferenceStartedEvent{Model: "test-model"})

	// Now that it's asynchronous, bus.Publish doesn't return an error from the actor's panic.
	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond) // Wait for actor loop to panic and recover

	// Phase 2: The Recovery/Follow-up
	mock.shouldPanic = false
	uniqueMsg := "recovered and processing"
	err = bus.Publish(ctx, events.StatusUpdate{Message: uniqueMsg, Level: "info"})

	assert.NoError(t, err)
	time.Sleep(20 * time.Millisecond) // Wait for second event processing
	assert.Equal(t, uniqueMsg, mock.lastMsg)
	assert.Equal(t, "info", mock.lastLevel)
	bridge.Cleanup()
}
