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
	return &b.WG
}
