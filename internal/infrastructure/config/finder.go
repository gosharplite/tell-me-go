// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// defaultConfigFinder implements the domain's ConfigFinder interface with tiered search logic.
type defaultConfigFinder struct {
	baseDir string
}

// option defines a functional option for defaultConfigFinder.
type option func(*defaultConfigFinder)

// WithBaseDir sets the base directory for search operations.
func WithBaseDir(dir string) option {
	return func(f *defaultConfigFinder) {
		f.baseDir = dir
	}
}

// NewDefaultConfigFinder creates a new defaultConfigFinder instance with optional configurations.
func NewDefaultConfigFinder(opts ...option) config.ConfigFinder {
	f := &defaultConfigFinder{}
	for _, opt := range opts {
		opt(f)
	}
	return f
}

// Find locates the configuration file by searching in several locations:
// 1. Local Directory: ./configs/assistant.yaml or ./assistant.yaml (relative to baseDir).
// 2. Executable Directory: Search for configs/assistant.yaml next to the binary.
// 3. Parent Traversal: Search for configs/assistant.yaml or .tell-me-go.yaml in up to 5 levels of parent directories.
// 4. Standard OS Config Paths: e.g., ~/.config/tell-me-go/assistant.yaml on Linux or %AppData%\tell-me-go\assistant.yaml on Windows.
// 5. Fallback: Returns "configs/assistant.yaml" if no file is found.
func (f *defaultConfigFinder) Find() (string, error) {
	// 1. Local Directory
	base := f.baseDir
	if base == "" {
		if wd, err := os.Getwd(); err == nil {
			base = wd
		} else {
			base = "."
		}
	}

	localPaths := []string{
		filepath.Join(base, "configs", "assistant.yaml"),
		filepath.Join(base, "assistant.yaml"),
	}
	for _, path := range localPaths {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 2. Executable Directory (for installed binaries or bundled configs)
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		// Avoid redundant search if exeDir is same as base
		if absBase, err := filepath.Abs(base); err == nil {
			if absExeDir, err := filepath.Abs(exeDir); err == nil && absBase != absExeDir {
				path := filepath.Join(exeDir, "configs", "assistant.yaml")
				if _, err := os.Stat(path); err == nil {
					return path, nil
				}
			}
		}
	}

	// 3. Parent Traversal (Upward)
	searchDir := base
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

	// 4. Standard OS Paths
	configDir, err := os.UserConfigDir()
	if err == nil {
		path := filepath.Join(configDir, "tell-me-go", "assistant.yaml")
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	// 5. Fallback
	return filepath.Join(base, "configs", "assistant.yaml"), nil
}

// Ensure defaultConfigFinder implements config.ConfigFinder
var _ config.ConfigFinder = (*defaultConfigFinder)(nil)
