// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

// WorkspacePolicy centralizes filesystem filtering rules so that every tool and
// service in the application agrees on which directories and paths to skip.
type WorkspacePolicy interface {
	// ShouldIgnoreDir reports whether a directory entry (by name only, not full
	// path) should be excluded from traversal. Callers that walk directory trees
	// should skip the entry and, if the entry is a directory, skip its children.
	ShouldIgnoreDir(name string) bool

	// ShouldIgnorePath reports whether a full path should be excluded. This is
	// used when the caller already has the resolved path (e.g. secret scanning)
	// and needs to check every path component against the policy.
	ShouldIgnorePath(path string) bool
}
