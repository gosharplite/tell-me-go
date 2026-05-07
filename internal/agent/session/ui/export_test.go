// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/mock"
)

// SyncBridge is exported for use by external test packages (package ui_test).
func SyncBridge(t *testing.T, b *Bridge, m interface {
	On(methodName string, arguments ...interface{}) *mock.Call
}) {
	syncBridge(t, b, m)
}

// Wg returns a pointer to the internal wait group for test synchronization.
func (b *Bridge) Wg() *sync.WaitGroup {
	return &b.wg
}

// SetBeforeBlockingSendHook installs a callback fired by enqueueCritical
// after the pre-guards (caller cancellation, actor death) but before the
// blocking select. Tests use this to deterministically observe when a
// goroutine is inside the select, replacing time.Sleep-based flaky sync.
//
// The hook is nil by default and has zero overhead in production.
func (b *Bridge) SetBeforeBlockingSendHook(fn func()) {
	b.queue.beforeBlockingSendHook = fn
}
