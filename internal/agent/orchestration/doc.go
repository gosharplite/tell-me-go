// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

/*
Package orchestration manages the session lifecycle and coordinates between the domain logic and the UI.

# UI Concurrency (Actor Model)

The uiBridge component within this package follows the Actor Model to handle asynchronous UI updates safely.
All events received from the domain are funneled into a single background goroutine (the "actor loop") via
a mailbox channel.

Rules for uiBridge:
  - All internal state (isRendering, activePhase, etc.) must remain private to the loop() goroutine.
  - sync.Mutex is strictly forbidden for state protection; use channels and events instead.
  - The component must be formally shut down using Cleanup() to prevent goroutine leaks.

# Deterministic Testing

Tests in this package must be 100% deterministic and are forbidden from using time.Sleep for synchronization.

Patterns for Testing:
  - Positive Tests: Use testify/mock's .Run() method to signal a channel when a mock is called.
  - Negative Tests: Use the bridge.sync() helper to flush the event queue. This sends a syncEvent
    through the actor loop, guaranteeing that all previously queued events have been processed before
    assertions (like AssertNotCalled) are made.
*/
package orchestration
