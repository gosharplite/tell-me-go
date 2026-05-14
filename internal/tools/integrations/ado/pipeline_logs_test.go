// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"testing"
	"time"
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
