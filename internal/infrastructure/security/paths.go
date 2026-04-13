// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// pathPolicy manages allowed boundaries and validates paths.
type pathPolicy struct {
	safePaths       map[string]struct{}
	safePathsMu     sync.RWMutex
	readOnlyPaths   map[string]struct{}
	readOnlyPathsMu sync.RWMutex
	resolvedTempDir string
}

// newPathPolicy creates a new pathPolicy.
func newPathPolicy(safePaths []string) *pathPolicy {
	policy := &pathPolicy{
		safePaths:     make(map[string]struct{}),
		readOnlyPaths: make(map[string]struct{}),
	}

	for _, p := range safePaths {
		if abs, err := filepath.Abs(p); err == nil {
			policy.safePaths[abs] = struct{}{}
		}
	}

	if temp := os.TempDir(); temp != "" {
		resolved, err := filepath.EvalSymlinks(temp)
		if err == nil {
			policy.resolvedTempDir = filepath.Clean(resolved) + string(filepath.Separator)
		} else {
			policy.resolvedTempDir = filepath.Clean(temp) + string(filepath.Separator)
		}
	}

	return policy
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

	// Existing: user-specific temp directory
	if ok, _ := p.checkBoundary(absPath, os.TempDir()); ok {
		return true, nil
	}

	// Platform-specific extra temp boundaries (e.g., /tmp, /private/tmp for Unix)
	for _, tmpDir := range getExtraTempDirs() {
		if ok, _ := p.checkBoundary(absPath, tmpDir); ok {
			return true, nil
		}
	}

	return false, nil
}

// checkSafePaths checks against the safePaths map.
func (p *pathPolicy) checkSafePaths(absPath string, _ bool) (bool, error) {
	p.safePathsMu.RLock()
	defer p.safePathsMu.RUnlock()

	for sp := range p.safePaths {
		if ok, _ := p.checkBoundary(absPath, sp); ok {
			return true, nil
		}
	}
	return false, nil
}

// checkReadOnlyPaths if writable is false, checks against the readOnlyPaths map.
func (p *pathPolicy) checkReadOnlyPaths(absPath string, writable bool) (bool, error) {
	if writable {
		return false, nil
	}

	p.readOnlyPathsMu.RLock()
	defer p.readOnlyPathsMu.RUnlock()

	for rop := range p.readOnlyPaths {
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
	return "", fmt.Errorf("%w: path '%s' is not in a %s boundary", domain_security.ErrSandboxViolation, path, mode)
}

func (p *pathPolicy) isSystemDirectory(absPath string) error {
	// 0. Normalize and resolve input path for internal consistency.
	// This ensures that short/long names on Windows and symlinks are handled
	// before prefix matching against system directories and exemptions.
	absPath = p.resolveSymlinks(absPath)
	absPath = filepath.ToSlash(absPath)

	// Explicitly exempt CWD and its children
	if cwd, err := os.Getwd(); err == nil {
		if ok, _ := p.checkBoundary(absPath, cwd); ok {
			return nil
		}
	}

	// 1. Explicitly exempt the evaluated OS temporary directory
	if p.resolvedTempDir != "" {
		temp := filepath.ToSlash(p.resolvedTempDir)
		if !isCaseSensitive() {
			temp = strings.ToLower(temp)
			abs := strings.ToLower(absPath)
			if strings.HasPrefix(abs, temp) {
				return nil
			}
		} else if strings.HasPrefix(absPath, temp) {
			return nil
		}
	}

	sensitive := getSystemDirectories()
	caseSensitive := isCaseSensitive()

	for _, s := range sensitive {
		sNormalized := filepath.ToSlash(s)
		sTarget := sNormalized
		sPrefix := sNormalized
		if !strings.HasSuffix(sPrefix, "/") {
			sPrefix += "/"
		}

		if !caseSensitive {
			sTarget = strings.ToLower(sTarget)
			sPrefix = strings.ToLower(sPrefix)
			absLower := strings.ToLower(absPath)
			if absLower == sTarget || strings.HasPrefix(absLower, sPrefix) {
				return fmt.Errorf("%w: access to system directory '%s' is forbidden", domain_security.ErrSandboxViolation, s)
			}
		} else {
			if absPath == sTarget || strings.HasPrefix(absPath, sPrefix) {
				return fmt.Errorf("%w: access to system directory '%s' is forbidden", domain_security.ErrSandboxViolation, s)
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
	cleaned := filepath.Clean(path)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return
	}

	if writable {
		p.safePathsMu.Lock()
		defer p.safePathsMu.Unlock()
		p.safePaths[abs] = struct{}{}
	} else {
		p.readOnlyPathsMu.Lock()
		defer p.readOnlyPathsMu.Unlock()
		p.readOnlyPaths[abs] = struct{}{}
	}
}

// RemovePath removes a path from the allowed boundaries.
func (p *pathPolicy) RemovePath(path string, writable bool) error {
	cleaned := filepath.Clean(path)
	abs, err := filepath.Abs(cleaned)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	var mu *sync.RWMutex
	var paths map[string]struct{}
	var mode string

	if writable {
		mu = &p.safePathsMu
		paths = p.safePaths
		mode = "safe"
	} else {
		mu = &p.readOnlyPathsMu
		paths = p.readOnlyPaths
		mode = "read-only"
	}

	mu.Lock()
	defer mu.Unlock()

	if _, ok := paths[abs]; !ok {
		return fmt.Errorf("path '%s' not found in %s authorized list", abs, mode)
	}
	delete(paths, abs)
	return nil
}

// GetPaths returns a copy of the registered paths.
func (p *pathPolicy) GetPaths(writable bool) []string {
	var paths map[string]struct{}

	if writable {
		p.safePathsMu.RLock()
		paths = p.safePaths
		defer p.safePathsMu.RUnlock()
	} else {
		p.readOnlyPathsMu.RLock()
		paths = p.readOnlyPaths
		defer p.readOnlyPathsMu.RUnlock()
	}

	res := make([]string, 0, len(paths))
	for path := range paths {
		res = append(res, path)
	}
	return res
}

func (p *pathPolicy) checkBoundary(target, boundary string) (bool, error) {
	absBoundary, err := filepath.Abs(boundary)
	if err != nil {
		return false, err
	}
	realBoundary := p.resolveSymlinks(absBoundary)

	rel, err := filepath.Rel(realBoundary, target)
	ok := err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
	return ok, nil
}

func (p *pathPolicy) resolveSymlinks(path string) string {
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}

	dir := filepath.Dir(path)
	if dir == path || dir == "." {
		return path
	}

	resolvedDir := p.resolveSymlinks(dir)
	return filepath.Join(resolvedDir, filepath.Base(path))
}
