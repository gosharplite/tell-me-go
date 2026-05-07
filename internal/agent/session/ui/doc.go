// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package ui implements the terminal user interface bridge. It receives
// structured events from the agent core and renders them through a
// pluggable UIRenderer, managing spinner lifecycle, output formatting,
// and backpressure-aware event queuing.
//
// # Test file conventions
//
//   - event_queue_test.go : unit tests for the private eventQueue type
//     (TestEventQueue_* prefix)
//
//   - bridge_test.go      : integration tests through Bridge's public API
//     (TestUIBridge_* prefix)
//
//   - <topic>_test.go     : specialized integration scenarios grouped by topic
//     (backpressure, concurrency, consent, panic, etc.)
package ui
