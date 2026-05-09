// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import "github.com/gosharplite/tell-me-go/internal/domain/events"

// ConfigWatcher defines the interface for monitoring configuration.
type ConfigWatcher interface {
	SetPaths(main, session string)

	// Refresh re-reads configuration from the underlying source (file, env,
	// or in-memory store) using modelHint to disambiguate model-specific
	// overrides. It is invoked by (*agent).applyConfig before the fallible
	// delegate chain runs.
	//
	// Refresh is intentionally void per ADR-029 §5: it implements best-effort
	// reload semantics. A failed refresh leaves the watcher's prior state
	// intact, which is acceptable because the next chat turn will retry
	// (idempotent reload). Promoting Refresh to fallible would force every
	// caller to decide between "abort the chat" and "log and continue" —
	// a policy choice the ADR explicitly defers.
	//
	// Do NOT change this signature to return error without first amending
	// ADR-029. The fail-fast delegate chain in (*agent).applyConfig is
	// scoped to SafePublish, Engine.Reconfigure, and Manager.Reconfigure
	// only; expanding the chain is a non-trivial architectural decision.
	Refresh(model string)
	SetLimits(tokens, toolTurns, historyTurns int)
	GetLimits() (tokens, toolTurns, historyTurns int)
	GetContextWindow() int
	ApplyLimits(l events.Limits)
}
