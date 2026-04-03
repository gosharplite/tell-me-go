// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

/*
Package session manages the session lifecycle and coordinates between the domain logic and the UI.

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
  - Outside-In Synchronization: Use mocks to signal when an operation completes. For negative assertions
    (AssertNotCalled), send a sentinel event that you know is handled by the bridge and wait for its
    corresponding mock call to ensure all previously queued events were processed.
  - Integration Tests: Use Eventually blocks to poll for expected side effects (e.g. stderr output).
*/
package session
