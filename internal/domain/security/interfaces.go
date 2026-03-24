// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "context"

type PathValidator interface {
	IsPathSafe(path string) (string, error)
	IsPathWritable(path string) (string, error)
}

type ActionConfirmer interface {
	// Authorize requests user confirmation for a potentially destructive or unsafe action.
	// This method is part of the public interface used across package boundaries
	// (e.g., by the agent executor and shell tools) and is not dead code.
	Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
}

type Auditor interface {
	LogAudit(action string, args ...any)
	Close() error
}

type TerminalController interface {
	TerminalLock()
	TerminalUnlock()
	Prompt(message string)
	Warn(message string)
	Confirm(ctx context.Context, message string) (bool, error)
	// ReadLine reads a line of input from the terminal. This method is part of the public interface
	// used across package boundaries (e.g., by workspace tools) and is not dead code.
	ReadLine(ctx context.Context) (string, error)
}

type PolicyEvaluator interface {
	IsCommandAllowed(command string) bool
	IsBypassActive() bool
}

type Manager interface {
	PathValidator
	ActionConfirmer
	Auditor
	TerminalController
	PolicyEvaluator
}

// UserInteractor defines the interface for user interactions (confirmations, warnings).
type UserInteractor interface {
	Confirm(ctx context.Context, message string) (bool, error)
	Warn(message string)
	Prompt(message string)
	ReadSingleKey(ctx context.Context) (string, error)
	// ReadLine reads a line of input from the terminal. This method is part of the public interface
	// used across package boundaries (e.g., by workspace tools) and is not dead code.
	ReadLine(ctx context.Context) (string, error)
}

// CommandValidator defines the interface for command validation.
type CommandValidator interface {
	IsSafe(command string) (bool, string)
	Split(cmd string) ([]string, error)
	ValidateStructure(parts []string) error
	CheckPathSafety(parts []string) (bool, string)
	HasShellFeatures(parts []string) bool
}
