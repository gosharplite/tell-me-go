// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package events

import (
	"context"
	"testing"
	"time"
)

// CleanupBus is a test helper that ensures the event bus is shut down properly.
func CleanupBus(t *testing.T, bus EventBus) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Logf("Warning: bus shutdown failed: %v", err)
		}
	})
}
