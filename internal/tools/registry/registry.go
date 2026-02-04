// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package registry maintains a mapping between function names and their Go implementations.
package registry

import (
	"context"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc = tools.ToolFunc

// ToolOptions defines execution behavior for a tool.
type ToolOptions struct {
	Serial      bool // If true, the agent waits for this tool to finish before running others.
	LongRunning bool // If true, the tool is exempt from default timeouts (e.g., interactive or heavy task).
}

// ToolEntry stores a tool's definition, handler, and execution options.
type ToolEntry struct {
	Declaration *tools.ToolDeclaration
	Handler     ToolFunc
	Options     ToolOptions
}

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	declarations []*tools.ToolDeclaration
	entries      map[string]ToolEntry
}

// New initializes an empty tool registry.
func New() *Registry {
	return &Registry{
		declarations: make([]*tools.ToolDeclaration, 0),
		entries:      make(map[string]ToolEntry),
	}
}

// Register adds a new tool to the registry with default options.
func (r *Registry) Register(def *tools.ToolDeclaration, handler ToolFunc) {
	r.RegisterWithOptions(def, handler, ToolOptions{})
}

// RegisterWithOptions adds a new tool to the registry with specific options.
func (r *Registry) RegisterWithOptions(def *tools.ToolDeclaration, handler ToolFunc, opts ToolOptions) {
	r.declarations = append(r.declarations, def)
	r.entries[def.Name] = ToolEntry{
		Declaration: def,
		Handler:     handler,
		Options:     opts,
	}
}

// GetDeclarations returns the list of function declarations.
func (r *Registry) GetDeclarations() []*tools.ToolDeclaration {
	return r.declarations
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	entry, ok := r.entries[name]
	if !ok {
		return tools.ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	return entry.Handler(ctx, args)
}

// IsSerial returns true if the tool is configured for serial execution.
func (r *Registry) IsSerial(name string) bool {
	if entry, ok := r.entries[name]; ok {
		return entry.Options.Serial
	}
	return false
}

// IsLongRunning returns true if the tool is configured as long-running (no timeout).
func (r *Registry) IsLongRunning(name string) bool {
	if entry, ok := r.entries[name]; ok {
		return entry.Options.LongRunning
	}
	return false
}

// UnmarshalArgs helper converts map[string]interface{} to a target struct.
func UnmarshalArgs(args map[string]interface{}, target interface{}) error {
	return tools.UnmarshalArgs(args, target)
}
