// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
)

// Package-level variables to allow tests to inject OS-call failures.
var (
	osGetwd         = os.Getwd
	osExecutable    = os.Executable
	osUserConfigDir = os.UserConfigDir
)

// defaultConfigFinder implements the domain's ConfigFinder interface with tiered search logic.
type defaultConfigFinder struct {
	baseDir string
}

// searchStrategy represents a single discovery attempt.
type searchStrategy func() (string, bool)

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
	strategies := []searchStrategy{
		f.findInLocalDir,
		f.findInExecutableDir,
		f.findInParentDirs,
		f.findInSystemPaths,
	}

	for _, strategy := range strategies {
		if path, found := strategy(); found {
			return path, nil
		}
	}

	return f.getFallbackPath(), nil
}

// getBaseDir returns the base directory for search operations.
func (f *defaultConfigFinder) getBaseDir() string {
	if f.baseDir != "" {
		return f.baseDir
	}
	if wd, err := osGetwd(); err == nil {
		return wd
	}
	log.Printf("config: cannot resolve current working directory: falling back to '.'")
	return "."
}

// findInLocalDir searches in the local directory.
func (f *defaultConfigFinder) findInLocalDir() (string, bool) {
	base := f.getBaseDir()
	localPaths := []string{
		filepath.Join(base, "configs", "assistant.yaml"),
		filepath.Join(base, "assistant.yaml"),
	}
	for _, path := range localPaths {
		if _, err := os.Stat(path); err == nil {
			return path, true
		}
	}
	return "", false
}

// findInExecutableDir searches in the directory of the executable.
func (f *defaultConfigFinder) findInExecutableDir() (string, bool) {
	exe, err := osExecutable()
	if err != nil {
		log.Printf("config: cannot resolve executable path: %v", err)
		return "", false
	}

	exeDir := filepath.Dir(exe)
	base := f.getBaseDir()

	// Avoid redundant search if exeDir is same as base
	absBase, err1 := filepath.Abs(base)
	absExeDir, err2 := filepath.Abs(exeDir)
	if err1 == nil && err2 == nil && absBase == absExeDir {
		return "", false
	}

	path := filepath.Join(exeDir, "configs", "assistant.yaml")
	if _, err := os.Stat(path); err == nil {
		return path, true
	}

	return "", false
}

// findInParentDirs searches in up to 5 levels of parent directories.
func (f *defaultConfigFinder) findInParentDirs() (string, bool) {
	searchDir := f.getBaseDir()
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
				return path, true
			}
		}
	}
	return "", false
}

// findInSystemPaths searches in standard OS config paths.
func (f *defaultConfigFinder) findInSystemPaths() (string, bool) {
	configDir, err := osUserConfigDir()
	if err != nil {
		log.Printf("config: cannot resolve user config directory: %v", err)
		return "", false
	}

	path := filepath.Join(configDir, "tell-me-go", "assistant.yaml")
	if _, err := os.Stat(path); err == nil {
		return path, true
	}

	return "", false
}

// getFallbackPath returns the fallback configuration path.
func (f *defaultConfigFinder) getFallbackPath() string {
	return filepath.Join(f.getBaseDir(), "configs", "assistant.yaml")
}

// Ensure defaultConfigFinder implements config.ConfigFinder
var _ config.ConfigFinder = (*defaultConfigFinder)(nil)
