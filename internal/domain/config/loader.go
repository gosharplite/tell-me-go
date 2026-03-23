// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

// ConfigLoader defines the interface for loading configuration.
type ConfigLoader interface {
	Load(path string) (*Config, error)
}

// SessionConfig represents dynamic override limits loaded from a session file.
type SessionConfig struct {
	MaxHistoryTokens *int
	MaxToolTurns     *int
	MaxHistoryTurns  *int
}

// SessionLoader defines the interface for loading dynamic session overrides.
type SessionLoader interface {
	LoadSession(path string) (*SessionConfig, error)
}
