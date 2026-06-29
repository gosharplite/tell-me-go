// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/filepathutil"
)

// pathPolicy manages allowed boundaries and validates paths.
type pathPolicy struct {
	safePaths       map[string]struct{}
	safePathsMu     sync.RWMutex
	readOnlyPaths   map[string]struct{}
	readOnlyPathsMu sync.RWMutex
	resolvedTempDir string
	customRules     []pathRule // custom path validation rules (ADR #830 safety net)
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

// addPathRule registers a custom path validation rule.
// Custom rules are evaluated before built-in rules.
// This is an internal extension point per ADR #830.
func (p *pathPolicy) addPathRule(rule pathRule) {
	p.customRules = append(p.customRules, rule)
}

type pathRule func(absPath string, writable bool) (bool, error)

// checkDefaultBoundaries checks if the path is within the Current Working Directory (CWD) or the system Temp directory.
func (p *pathPolicy) checkDefaultBoundaries(absPath string, _ bool) (bool, error) {
	cwd, err := os.Getwd()
	if err == nil {
		if p.tryBoundary(absPath, cwd) {
			return true, nil
		}
	}

	if p.tryBoundary(absPath, os.TempDir()) {
		return true, nil
	}

	for _, tmpDir := range getExtraTempDirs() {
		if p.tryBoundary(absPath, tmpDir) {
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
		ok, err := p.checkBoundary(absPath, sp)
		if err != nil {
			log.Printf("security: boundary check error for safe path %s against %s: %v", sp, absPath, err)
		}
		if ok {
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
		ok, err := p.checkBoundary(absPath, rop)
		if err != nil {
			log.Printf("security: boundary check error for read-only path %s against %s: %v", rop, absPath, err)
		}
		if ok {
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
	absPath = filepathutil.NormalizePath(absPath)

	if err := p.isSystemDirectory(absPath); err != nil {
		return "", err
	}

	rules := make([]pathRule, 0, len(p.customRules)+3)
	rules = append(rules, p.customRules...)
	rules = append(rules, p.checkDefaultBoundaries, p.checkSafePaths, p.checkReadOnlyPaths)

	for _, rule := range rules {
		ok, err := rule(absPath, writable)
		if err != nil {
			// NOTE: This error-propagation path is currently dead code because
			// all three built-in rules (checkDefaultBoundaries, checkSafePaths,
			// checkReadOnlyPaths) use log-and-continue semantics — they log
			// checkBoundary errors via log.Printf and return nil. This ensures
			// all boundaries are checked and all errors are logged before
			// rejecting the path (fail-secure). The path exists as a safety
			// net for custom pathRule implementations that may propagate errors.
			// See ADR #830 for the architectural decision.
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
	absPath = filepathutil.Normalize(absPath)

	if p.isExemptedDirectory(absPath) {
		return nil
	}

	sensitive := getSystemDirectories()
	caseSensitive := isCaseSensitive()

	for _, s := range sensitive {
		if p.checkSystemDirectoryMatch(absPath, s, caseSensitive) {
			return fmt.Errorf("%w: access to system directory '%s' is forbidden", domain_security.ErrSandboxViolation, s)
		}
	}
	return nil
}

// isExemptedDirectory checks if the path is in the CWD or Temp directory.
func (p *pathPolicy) isExemptedDirectory(absPath string) bool {
	// Explicitly exempt CWD and its children
	if cwd, err := os.Getwd(); err == nil {
		if ok, _ := p.checkBoundary(absPath, cwd); ok {
			return true
		}
	}

	// Explicitly exempt the evaluated OS temporary directory
	if p.isTempDirExempted(absPath, isCaseSensitive()) {
		return true
	}

	return false
}

// isTempDirExempted checks if absPath is within the resolved temporary directory.
// The caseSensitive parameter controls whether path comparison is case-sensitive.
func (p *pathPolicy) isTempDirExempted(absPath string, caseSensitive bool) bool {
	if p.resolvedTempDir == "" {
		return false
	}
	temp := filepath.ToSlash(p.resolvedTempDir)
	absNormalized := filepath.ToSlash(absPath)
	if !caseSensitive {
		temp = strings.ToLower(temp)
		abs := strings.ToLower(absNormalized)
		return strings.HasPrefix(abs, temp)
	}
	return strings.HasPrefix(absNormalized, temp)
}

// checkSystemDirectoryMatch normalizes a system directory and compares it against absPath.
func (p *pathPolicy) checkSystemDirectoryMatch(absPath, sysDir string, caseSensitive bool) bool {
	sNormalized := filepath.ToSlash(sysDir)
	sTarget := sNormalized
	sPrefix := sNormalized
	if !strings.HasSuffix(sPrefix, "/") {
		sPrefix += "/"
	}

	if !caseSensitive {
		sTarget = strings.ToLower(sTarget)
		sPrefix = strings.ToLower(sPrefix)
		absLower := strings.ToLower(absPath)
		return absLower == sTarget || strings.HasPrefix(absLower, sPrefix)
	}

	return absPath == sTarget || strings.HasPrefix(absPath, sPrefix)
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
	realBoundary := filepathutil.NormalizePath(absBoundary)
	realTarget := filepathutil.NormalizePath(target)

	rel, err := filepath.Rel(realBoundary, realTarget)
	ok := err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
	return ok, nil
}

// tryBoundary checks if absPath is within boundary, logging any error.
// Returns true if the path is within the boundary.
func (p *pathPolicy) tryBoundary(absPath, boundary string) bool {
	ok, err := p.checkBoundary(absPath, boundary)
	if err != nil {
		log.Printf("security: boundary check error for %s against %s: %v", boundary, absPath, err)
	}
	return ok
}

// resolveSymlinks delegates to filepathutil.NormalizePath for centralized
// cross-platform symlink resolution with recursive fallback.
func (p *pathPolicy) resolveSymlinks(path string) string {
	return filepathutil.NormalizePath(path)
}
