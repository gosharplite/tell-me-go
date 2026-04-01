// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package orchestration

import (
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type silentT struct{}

func (s *silentT) Errorf(format string, args ...interface{}) {}
func (s *silentT) FailNow()                                {}
func (s *silentT) Logf(format string, args ...interface{})   {}

func waitMock(t *testing.T, m *mock.Mock, timeout time.Duration) {
	t.Helper()
	require.Eventually(t, func() bool {
		return m.AssertExpectations(&silentT{})
	}, timeout, 10*time.Millisecond, "Mock expectations were not met")
}

func syncBridge(t *testing.T, b *uiBridge, m *mockUIRenderer) {
	t.Helper()
	// Use a sentinel event that is handled by the bridge and calls a mock method.
	// LogSystemMessage is ideal as it's safe to call when no spinner is active.
	m.On("LogSystemMessage", "SYNC_SENTINEL", "info").Return().Once()

	// Use a robust polling mechanism to force the sentinel into the queue.
	// This bypasses backpressure/load-shedding in handleEvent to guarantee sync.
	require.Eventually(t, func() bool {
		select {
		case b.eventCh <- events.SystemMessageEvent{Message: "SYNC_SENTINEL", Level: "info"}:
			return true
		default:
			return false // Queue is full, retry
		}
	}, 2*time.Second, 10*time.Millisecond, "Failed to queue sync sentinel")

	waitMock(t, &m.Mock, 2*time.Second)
}
