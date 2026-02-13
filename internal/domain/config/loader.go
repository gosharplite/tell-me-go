// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

// ConfigLoader defines the interface for loading configuration.
type ConfigLoader interface {
	Load(path string) (*Config, error)
}
