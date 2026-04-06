// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package session

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/mock"
)

func TestUIBridge_TurnsLogger(t *testing.T) {
	ctx := context.Background()

	t.Run("LogTurnStatus forwarded", func(t *testing.T) {
		mRenderer := new(mockUIRenderer)
		mLogger := new(mockTurnsLogger)
		bridge := newUIBridge(mRenderer, withBridgeTurnsLogger(mLogger))
		bridge.Start(ctx)
		defer func() {
			bridge.CloseInput()
			bridge.Cleanup()
		}()

		status := events.TurnStatus{Mode: "test"}
		event := events.TurnStatusEvent{Status: status}

		mRenderer.On("LogTurnStatus", mock.Anything, status).Return().Once()
		mLogger.On("LogTurnStatus", status, mock.Anything).Return().Once()

		_ = bridge.handleEvent(ctx, event)
		syncBridge(t, bridge, mRenderer)

		mRenderer.AssertExpectations(t)
		mLogger.AssertExpectations(t)
	})

	t.Run("LogSystemMessage forwarded", func(t *testing.T) {
		mRenderer := new(mockUIRenderer)
		mLogger := new(mockTurnsLogger)
		bridge := newUIBridge(mRenderer, withBridgeTurnsLogger(mLogger))
		bridge.Start(ctx)
		defer func() {
			bridge.CloseInput()
			bridge.Cleanup()
		}()

		msg, lvl := "hello", "info"
		event := events.SystemMessageEvent{Message: msg, Level: lvl}

		mRenderer.On("LogSystemMessage", mock.Anything, msg, lvl).Return().Once()
		mLogger.On("LogSystemMessage", msg, lvl, mock.Anything).Return().Once()

		_ = bridge.handleEvent(ctx, event)
		syncBridge(t, bridge, mRenderer)

		mRenderer.AssertExpectations(t)
		mLogger.AssertExpectations(t)
	})
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
