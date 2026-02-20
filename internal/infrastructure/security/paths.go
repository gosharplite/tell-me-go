// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
)

// pathPolicy manages allowed boundaries and validates paths.
type pathPolicy struct {
	safePaths         []string
	safePathsMu       sync.RWMutex
	safePathsFile     string
	readOnlyPaths     []string
	readOnlyPathsMu   sync.RWMutex
	readOnlyPathsFile string
}

// newPathPolicy creates a new pathPolicy.
func newPathPolicy() *pathPolicy {
	return &pathPolicy{}
}

type pathRule func(absPath string, writable bool) (bool, error)

// checkDefaultBoundaries checks if the path is within the Current Working Directory (CWD) or the system Temp directory.
func (p *pathPolicy) checkDefaultBoundaries(absPath string, _ bool) (bool, error) {
	cwd, err := os.Getwd()
	if err == nil {
		if ok, _ := p.checkBoundary(absPath, cwd); ok {
			return true, nil
		}
	}

	if ok, _ := p.checkBoundary(absPath, os.TempDir()); ok {
		return true, nil
	}
	return false, nil
}

// checkSafePaths checks against the safePaths slice, including the prevention of direct access to the safePathsFile.
func (p *pathPolicy) checkSafePaths(absPath string, _ bool) (bool, error) {
	p.safePathsMu.RLock()
	defer p.safePathsMu.RUnlock()

	if p.safePathsFile != "" {
		if absSafeFile, err := filepath.Abs(p.safePathsFile); err == nil && absPath == absSafeFile {
			return false, fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	for _, sp := range p.safePaths {
		if ok, _ := p.checkBoundary(absPath, sp); ok {
			return true, nil
		}
	}
	return false, nil
}

// checkReadOnlyPaths if writable is false, checks against the readOnlyPaths slice, including the prevention of direct access to the readOnlyPathsFile.
func (p *pathPolicy) checkReadOnlyPaths(absPath string, writable bool) (bool, error) {
	if writable {
		return false, nil
	}

	p.readOnlyPathsMu.RLock()
	defer p.readOnlyPathsMu.RUnlock()

	if p.readOnlyPathsFile != "" {
		if absReadSafeFile, err := filepath.Abs(p.readOnlyPathsFile); err == nil && absPath == absReadSafeFile {
			return false, fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
		}
	}

	for _, rop := range p.readOnlyPaths {
		if ok, _ := p.checkBoundary(absPath, rop); ok {
			return true, nil
		}
	}
	return false, nil
}

// ValidatePath checks if a path is allowed.
// If writable=true, it checks CWD, Temp, and SafePaths.
// If writable=false, it ALSO checks ReadOnlyPaths.
func (p *pathPolicy) ValidatePath(path string, writable bool) (string, error) {
	if path == "" {
		return "", nil
	}

	// 1. Hardened Sanitation
	cleanedPath := filepath.Clean(path)

	absPath, err := filepath.Abs(cleanedPath)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absPath = p.resolveSymlinks(absPath)

	if err := p.isSystemDirectory(absPath); err != nil {
		return "", err
	}

	rules := []pathRule{
		p.checkDefaultBoundaries,
		p.checkSafePaths,
		p.checkReadOnlyPaths,
	}

	for _, rule := range rules {
		ok, err := rule(absPath, writable)
		if err != nil {
			return "", err
		}
		if ok {
			return absPath, nil
		}
	}

	mode := "read-only"
	if writable {
		mode = "writable"
	}
	return "", fmt.Errorf("security violation: path '%s' is not in a %s boundary", path, mode)
}

func (p *pathPolicy) isSystemDirectory(absPath string) error {
	// Simple check for sensitive system directories on Unix
	sensitive := []string{"/etc", "/usr", "/bin", "/sbin", "/var", "/root", "/boot", "/dev", "/proc", "/sys"}
	for _, s := range sensitive {
		if absPath == s || strings.HasPrefix(absPath, s+"/") {
			// Special exception for /tmp handled by checkDefaultBoundaries
			if !strings.HasPrefix(absPath, "/tmp") {
				return fmt.Errorf("security violation: access to system directory '%s' is forbidden", s)
			}
		}
	}
	return nil
}

// RegisterPath adds a path to the allowed boundaries.
func (p *pathPolicy) RegisterPath(path string, writable bool) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}

	if writable {
		p.safePathsMu.Lock()
		defer p.safePathsMu.Unlock()
		for _, sp := range p.safePaths {
			if sp == abs {
				return
			}
		}
		p.safePaths = append(p.safePaths, abs)
	} else {
		p.readOnlyPathsMu.Lock()
		defer p.readOnlyPathsMu.Unlock()
		for _, rop := range p.readOnlyPaths {
			if rop == abs {
				return
			}
		}
		p.readOnlyPaths = append(p.readOnlyPaths, abs)
	}
}

// RemovePath removes a path from the allowed boundaries.
func (p *pathPolicy) RemovePath(path string, writable bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	var mu *sync.RWMutex
	var paths *[]string
	var mode string

	if writable {
		mu = &p.safePathsMu
		paths = &p.safePaths
		mode = "safe"
	} else {
		mu = &p.readOnlyPathsMu
		paths = &p.readOnlyPaths
		mode = "read-only"
	}

	mu.Lock()
	defer mu.Unlock()

	newPaths := []string{}
	found := false
	for _, entry := range *paths {
		if entry == abs {
			found = true
			continue
		}
		newPaths = append(newPaths, entry)
	}

	if !found {
		return fmt.Errorf("path '%s' not found in %s authorized list", abs, mode)
	}

	*paths = newPaths
	return nil
}

// GetPaths returns a copy of the registered paths.
func (p *pathPolicy) GetPaths(writable bool) []string {
	var mu *sync.RWMutex
	var paths []string

	if writable {
		mu = &p.safePathsMu
		p.safePathsMu.RLock()
		paths = p.safePaths
	} else {
		mu = &p.readOnlyPathsMu
		p.readOnlyPathsMu.RLock()
		paths = p.readOnlyPaths
	}
	defer mu.RUnlock()

	res := make([]string, len(paths))
	copy(res, paths)
	return res
}

// SetConfigFile sets the persistence file for paths.
func (p *pathPolicy) SetConfigFile(path string, writable bool) {
	if writable {
		p.safePathsMu.Lock()
		p.safePathsFile = path
		p.safePathsMu.Unlock()
	} else {
		p.readOnlyPathsMu.Lock()
		p.readOnlyPathsFile = path
		p.readOnlyPathsMu.Unlock()
	}
}

// LoadPaths reads paths from the config file.
func (p *pathPolicy) LoadPaths(writable bool) error {
	var file string
	if writable {
		p.safePathsMu.RLock()
		file = p.safePathsFile
		p.safePathsMu.RUnlock()
	} else {
		p.readOnlyPathsMu.RLock()
		file = p.readOnlyPathsFile
		p.readOnlyPathsMu.RUnlock()
	}

	if file == "" {
		return nil
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return fmt.Errorf("failed to parse JSON: %w", err)
	}

	for _, path := range paths {
		p.RegisterPath(path, writable)
	}
	return nil
}

// SavePaths writes paths to the config file.
func (p *pathPolicy) SavePaths(ctx context.Context, writable bool) error {
	var file string
	var paths []string

	if writable {
		p.safePathsMu.RLock()
		file = p.safePathsFile
		paths = make([]string, len(p.safePaths))
		copy(paths, p.safePaths)
		p.safePathsMu.RUnlock()
	} else {
		p.readOnlyPathsMu.RLock()
		file = p.readOnlyPathsFile
		paths = make([]string, len(p.readOnlyPaths))
		copy(paths, p.readOnlyPaths)
		p.readOnlyPathsMu.RUnlock()
	}

	if file == "" {
		return nil
	}

	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal paths: %w", err)
	}

	return persistence.AtomicWrite(ctx, file, data, 0644)
}

func (p *pathPolicy) checkBoundary(target, boundary string) (bool, error) {
	absBoundary, err := filepath.Abs(boundary)
	if err != nil {
		return false, err
	}
	realBoundary := p.resolveSymlinks(absBoundary)

	rel, err := filepath.Rel(realBoundary, target)
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel), nil
}

func (p *pathPolicy) resolveSymlinks(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}
	dir := filepath.Dir(path)
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(realDir, filepath.Base(path))
	}
	return path
}
