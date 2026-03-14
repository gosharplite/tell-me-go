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
	//nolint:unused // False positive: Implicitly used via interface embedding across boundaries.
	Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error)
}

type Auditor interface {
	LogAudit(label1, val1, label2, val2 string)
}

type TerminalController interface {
	TerminalLock()
	TerminalUnlock()
	Prompt(message string)
	Warn(message string)
	Confirm(ctx context.Context, message string) (bool, error)
	// ReadLine reads a line of input from the terminal. This method is part of the public interface
	// used across package boundaries (e.g., by workspace tools) and is not dead code.
	//nolint:unused // False positive: Implicitly used via interface embedding across boundaries.
	ReadLine(ctx context.Context) (string, error)
}

type PolicyEvaluator interface {
	IsCommandAllowed(command string) bool
	IsBypassActive() bool
}

type ISecurityManager interface {
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
	//nolint:unused // False positive: Implicitly used via interface embedding across boundaries.
	ReadLine(ctx context.Context) (string, error)
}

// ICommandValidator defines the interface for command validation.
type ICommandValidator interface {
	IsSafe(command string) (bool, string)
	Split(cmd string) ([]string, error)
	ValidateStructure(parts []string) error
	CheckPathSafety(parts []string) (bool, string)
}

// Compile-time interface method expressions.
// These create hard AST references to ensure AST-based callgraph tools (like dead_code_graph)
// do not falsely flag these public interface methods as dead code when they are consumed
// implicitly via structural typing and local interface embedding across package boundaries.
var (
	_ = ActionConfirmer.Authorize
	_ = TerminalController.ReadLine
	_ = UserInteractor.ReadLine
)
