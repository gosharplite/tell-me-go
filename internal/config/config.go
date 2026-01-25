// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration loaded from a YAML file.
type Config struct {
	Mode             string `yaml:"MODE"`
	Person           string `yaml:"PERSON"`
	URL              string `yaml:"AIURL"`
	Model            string `yaml:"AIMODEL"`
	UseSearch        bool   `yaml:"USE_SEARCH"`
	MaxToolTurns     int    `yaml:"MAX_TURNS"`          // Renamed for clarity in logic, kept YAML tag for compatibility
	MaxHistoryTurns  int    `yaml:"MAX_HISTORY_TURNS"`  // New: For pruning turns
	MaxHistoryTokens int    `yaml:"MAX_HISTORY_TOKENS"` // New: For safety rollback
	ThinkingBudget   int    `yaml:"THINKING_BUDGET"`
	ThinkingLevel    string `yaml:"THINKING_LEVEL"`
}

// Load reads and parses the configuration file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	// Set defaults
	cfg.MaxToolTurns = 10
	cfg.MaxHistoryTurns = 20
	cfg.MaxHistoryTokens = 120000

	decoder := yaml.NewDecoder(f)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("failed to decode yaml: %w", err)
	}

	return &cfg, nil
}
