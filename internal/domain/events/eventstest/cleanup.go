// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package eventstest

import (
	"context"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
)

// CleanupBus registers a t.Cleanup hook that shuts the supplied bus
// down with a 2s deadline when the test (or subtest) completes.
//
// Lives in eventstest rather than the production events package
// because it imports "testing", which ADR-021 forbids in production
// code. See ADR-022.
func CleanupBus(t *testing.T, bus events.EventBus) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := bus.Shutdown(ctx); err != nil {
			t.Logf("Warning: bus shutdown failed: %v", err)
		}
	})
}
