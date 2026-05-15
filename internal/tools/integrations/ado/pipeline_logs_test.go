// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSendScannerHeartbeat(t *testing.T) {
	t.Run("nil channel does nothing", func(t *testing.T) {
		// Should not panic when hb is nil
		sendScannerHeartbeat(nil, 1000)
		sendScannerHeartbeat(nil, 2000)
	})

	t.Run("non-multiple-of-1000 does not send", func(t *testing.T) {
		hb := make(chan struct{}, 1)
		sendScannerHeartbeat(hb, 999)
		select {
		case <-hb:
			t.Fatal("unexpected heartbeat for count 999")
		default:
			// expected — no heartbeat
		}
	})

	t.Run("sends heartbeat at count 1000", func(t *testing.T) {
		hb := make(chan struct{}, 1)
		sendScannerHeartbeat(hb, 1000)
		select {
		case <-hb:
			// expected
		default:
			t.Fatal("expected heartbeat for count 1000")
		}
	})

	t.Run("full channel drops heartbeat gracefully", func(t *testing.T) {
		hb := make(chan struct{}) // unbuffered, no receiver
		// This should not block or panic — the default case handles it
		done := make(chan struct{})
		go func() {
			sendScannerHeartbeat(hb, 1000)
			close(done)
		}()
		select {
		case <-done:
			// expected — didn't block
		case <-time.After(100 * time.Millisecond):
			t.Fatal("sendScannerHeartbeat blocked on full channel")
		}
	})
}

func TestNewLogFilterState(t *testing.T) {
	tests := []struct {
		name             string
		contextLines     int
		wantContextLines int
		wantPreWinNil    bool
		wantPreWinLen    int
	}{
		{
			name:             "Default context lines (5)",
			contextLines:     5,
			wantContextLines: 5,
			wantPreWinNil:    false,
			wantPreWinLen:    5,
		},
		{
			name:             "Zero context lines",
			contextLines:     0,
			wantContextLines: 0,
			wantPreWinNil:    true,
			wantPreWinLen:    0,
		},
		{
			name:             "Negative context lines defaults to 5",
			contextLines:     -1,
			wantContextLines: 5,
			wantPreWinNil:    false,
			wantPreWinLen:    5,
		},
		{
			name:             "Context lines at boundary 100",
			contextLines:     100,
			wantContextLines: 100,
			wantPreWinNil:    false,
			wantPreWinLen:    100,
		},
		{
			name:             "Context lines exceeds 100 clamps to 100",
			contextLines:     150,
			wantContextLines: 100,
			wantPreWinNil:    false,
			wantPreWinLen:    100,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			state := newLogFilterState(tt.contextLines)
			assert.Equal(t, tt.wantContextLines, state.contextLines)
			if tt.wantPreWinNil {
				assert.Nil(t, state.preWindow)
			} else {
				assert.Len(t, state.preWindow, tt.wantPreWinLen)
			}
		})
	}
}
