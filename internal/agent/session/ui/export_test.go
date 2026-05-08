// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"context"
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

// StartListen is the exported wrapper around startListen for use by external
// test packages (package ui_test). It launches bridge.Listen in a goroutine,
// registers a t.Cleanup that surfaces non-cancellation errors, and returns
// the listen context, its cancel function, and a channel that closes when
// Listen exits.
func StartListen(t *testing.T, b *Bridge) (context.Context, context.CancelFunc, <-chan struct{}) {
	t.Helper()
	return startListen(t, b)
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

// SetPanicHook installs a callback fired by enqueueNonCritical inside the
// default (load-shedding) branch, after the pre-guards but before the debug
// log. Tests use this to inject a panic for verifying HandleEvent's
// defer/recover safety net.
//
// The hook is nil by default and has zero overhead in production.
func (b *Bridge) SetPanicHook(fn func()) {
	b.queue.panicHook = fn
}
