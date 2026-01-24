// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package config handles application configuration loading and parsing.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config represents the application configuration.
type Config struct {
	Mode           string `yaml:"MODE"`
	Person         string `yaml:"PERSON"`
	Model          string `yaml:"AIMODEL"`
	URL            string `yaml:"AIURL"`
	UseSearch      bool   `yaml:"USE_SEARCH"`
	ThinkingBudget int    `yaml:"THINKING_BUDGET"`
	MaxTurns       int    `yaml:"MAX_TURNS"`
	KeyFile        string `yaml:"KEY_FILE"`
	APIKey         string `yaml:"API_KEY"` // For AI Studio
}

// Load reads a YAML configuration file from the given path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse yaml: %w", err)
	}

	return &cfg, nil
}
