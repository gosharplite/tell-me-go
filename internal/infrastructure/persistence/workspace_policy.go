// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/services"
)

// Ensure defaultWorkspacePolicy satisfies the domain interface at compile time.
var _ services.WorkspacePolicy = (*defaultWorkspacePolicy)(nil)

// defaultWorkspacePolicy is the standard implementation of WorkspacePolicy.
// It uses the union of all ignore rules that were previously duplicated across
// the codebase (tools, suggestions, release scanning). It is safe for concurrent
// use because it has no mutable state.
type defaultWorkspacePolicy struct{}

// NewWorkspacePolicy returns a new defaultWorkspacePolicy.
func NewWorkspacePolicy() services.WorkspacePolicy {
	return &defaultWorkspacePolicy{}
}

// ignoredDirNames is the canonical set of directory names that every walker and
// scanner in the application must skip. It is the union of all previously
// hardcoded lists.
var ignoredDirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	"bin":          true,
	"obj":          true,
	"output":       true,
	"dist":         true,
	"testdata":     true,
	"configs":      true,
}

// ignoredExtensions lists file suffixes that the path-level policy should skip
// (used by secret scanning and other full-path checks).
var ignoredExtensions = []string{
	"_test.go",
	".md",
	".json",
	".golden",
}

// ShouldIgnoreDir reports whether the directory name should be skipped. It
// matches the explicit set of known noise directories and also any hidden
// directory (name starts with '.').
func (p *defaultWorkspacePolicy) ShouldIgnoreDir(name string) bool {
	if ignoredDirNames[name] {
		return true
	}
	// Hidden directories (e.g. .terraform, .vscode) — but "." itself is not ignored.
	return len(name) > 1 && name[0] == '.'
}

// ShouldIgnorePath reports whether the full path should be skipped. It checks
// every path component against the directory policy and also checks the leaf
// file against the ignored extensions list.
func (p *defaultWorkspacePolicy) ShouldIgnorePath(path string) bool {
	normalized := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(normalized, "/")
	for _, part := range parts {
		if p.ShouldIgnoreDir(part) {
			return true
		}
	}
	// Check file extension exclusions on the leaf.
	leaf := parts[len(parts)-1]
	for _, ext := range ignoredExtensions {
		if strings.HasSuffix(leaf, ext) {
			return true
		}
	}
	return false
}
