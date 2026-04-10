// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// DefaultConfigFinder implements the domain's ConfigFinder interface with tiered search logic.
type DefaultConfigFinder struct{}

// NewDefaultConfigFinder creates a new DefaultConfigFinder instance.
func NewDefaultConfigFinder() *DefaultConfigFinder {
	return &DefaultConfigFinder{}
}

// Find locates the configuration file by searching in several locations:
// 1. Local Directory: ./configs/assistant.yaml or ./assistant.yaml.
// 2. Parent Traversal: Search for configs/assistant.yaml or .tell-me-go.yaml in up to 5 levels of parent directories.
// 3. Standard OS Config Paths: e.g., ~/.config/tell-me-go/assistant.yaml on Linux or %AppData%\tell-me-go\assistant.yaml on Windows.
// 4. Fallback: Returns "configs/assistant.yaml" if no file is found.
func (f *DefaultConfigFinder) Find() (string, error) {
	// 1. Local Directory
	localPaths := []string{
		filepath.Join("configs", "assistant.yaml"),
		"assistant.yaml",
	}
	for _, path := range localPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 2. Parent Traversal (Upward)
	currentDir, err := os.Getwd()
	if err == nil {
		searchDir := currentDir
		for i := 0; i < 5; i++ {
			parentDir := filepath.Dir(searchDir)
			if parentDir == searchDir {
				break
			}
			searchDir = parentDir
			
			paths := []string{
				filepath.Join(searchDir, "configs", "assistant.yaml"),
				filepath.Join(searchDir, ".tell-me-go.yaml"),
			}
			for _, path := range paths {
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
	}

	// 3. Standard OS Paths
	configDir, err := os.UserConfigDir()
	if err == nil {
		path := filepath.Join(configDir, "tell-me-go", "assistant.yaml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 4. Fallback
	return "configs/assistant.yaml", nil
}

// Ensure DefaultConfigFinder implements config.ConfigFinder
var _ config.ConfigFinder = (*DefaultConfigFinder)(nil)
