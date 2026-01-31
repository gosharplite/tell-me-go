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

	"github.com/gosharplite/tell-me-go/internal/fsutil"
)

// PathPolicy manages allowed boundaries and validates paths.
type PathPolicy struct {
	safePaths         []string
	safePathsMu       sync.RWMutex
	safePathsFile     string
	readOnlyPaths     []string
	readOnlyPathsMu   sync.RWMutex
	readOnlyPathsFile string
}

// NewPathPolicy creates a new PathPolicy.
func NewPathPolicy() *PathPolicy {
	return &PathPolicy{}
}

// ValidatePath checks if a path is allowed.
// If writable=true, it checks CWD, Temp, and SafePaths.
// If writable=false, it ALSO checks ReadOnlyPaths.
func (p *PathPolicy) ValidatePath(path string, writable bool) (string, error) {
	if path == "" {
		return "", nil
	}

	// 1. Hardened Sanitation
	path = filepath.Clean(path)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %w", err)
	}
	absPath = p.resolveSymlinks(absPath)

	// 2. Default Boundaries (CWD, Temp)
	cwd, err := os.Getwd()
	if err == nil {
		if ok, _ := p.checkBoundary(absPath, cwd); ok {
			return absPath, nil
		}
	}

	if ok, _ := p.checkBoundary(absPath, os.TempDir()); ok {
		return absPath, nil
	}

	// 3. Safe Paths (Read + Write)
	p.safePathsMu.RLock()
	// Block access to config files
	if p.safePathsFile != "" {
		if absSafeFile, err := filepath.Abs(p.safePathsFile); err == nil && absPath == absSafeFile {
			p.safePathsMu.RUnlock()
			return "", fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	for _, sp := range p.safePaths {
		if ok, _ := p.checkBoundary(absPath, sp); ok {
			p.safePathsMu.RUnlock()
			return absPath, nil
		}
	}
	p.safePathsMu.RUnlock()

	// 4. Read-Only Paths (Read only)
	if !writable {
		p.readOnlyPathsMu.RLock()
		if p.readOnlyPathsFile != "" {
			if absReadSafeFile, err := filepath.Abs(p.readOnlyPathsFile); err == nil && absPath == absReadSafeFile {
				p.readOnlyPathsMu.RUnlock()
				return "", fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
			}
		}

		for _, rop := range p.readOnlyPaths {
			if ok, _ := p.checkBoundary(absPath, rop); ok {
				p.readOnlyPathsMu.RUnlock()
				return absPath, nil
			}
		}
		p.readOnlyPathsMu.RUnlock()
	}

	mode := "read-only"
	if writable {
		mode = "writable"
	}
	return "", fmt.Errorf("security violation: path '%s' is not in a %s boundary", path, mode)
}

// RegisterPath adds a path to the allowed boundaries.
func (p *PathPolicy) RegisterPath(path string, writable bool) {
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
func (p *PathPolicy) RemovePath(path string, writable bool) error {
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
func (p *PathPolicy) GetPaths(writable bool) []string {
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
func (p *PathPolicy) SetConfigFile(path string, writable bool) {
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
func (p *PathPolicy) LoadPaths(writable bool) error {
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
func (p *PathPolicy) SavePaths(ctx context.Context, writable bool) error {
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

	return fsutil.AtomicWrite(ctx, file, data, 0644)
}

func (p *PathPolicy) checkBoundary(target, boundary string) (bool, error) {
	absBoundary, err := filepath.Abs(boundary)
	if err != nil {
		return false, err
	}
	realBoundary := p.resolveSymlinks(absBoundary)

	rel, err := filepath.Rel(realBoundary, target)
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel), nil
}

func (p *PathPolicy) resolveSymlinks(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}
	dir := filepath.Dir(path)
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(realDir, filepath.Base(path))
	}
	return path
}
