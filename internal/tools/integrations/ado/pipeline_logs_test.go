// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSendScannerHeartbeat(t *testing.T) {
	tests := []struct {
		name         string
		hbSetup      func() chan struct{}
		lineCount    int
		wantReceived bool
	}{
		{
			name: "heartbeat sent on 1000th line",
			hbSetup: func() chan struct{} {
				return make(chan struct{}, 1)
			},
			lineCount:    1000,
			wantReceived: true,
		},
		{
			name: "no heartbeat on 999th line",
			hbSetup: func() chan struct{} {
				return make(chan struct{}, 1)
			},
			lineCount:    999,
			wantReceived: false,
		},
		{
			name: "nil channel does not panic",
			hbSetup: func() chan struct{} {
				return nil
			},
			lineCount:    1000,
			wantReceived: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			hb := tt.hbSetup()
			sendScannerHeartbeat(hb, tt.lineCount)
			if tt.wantReceived {
				assert.Equal(t, 1, len(hb), "expected heartbeat to be sent")
			} else if hb != nil {
				assert.Equal(t, 0, len(hb), "expected no heartbeat to be sent")
			}
			// nil channel case: nothing to assert, just verify no panic
		})
	}
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
