// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/gosharplite/tell-me-go/internal/types"
)

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(ctx context.Context, args map[string]interface{}) (types.ToolResult, error)

// ToolOptions defines execution behavior for a tool.
type ToolOptions struct {
	Serial      bool // If true, the agent waits for this tool to finish before running others.
	LongRunning bool // If true, the tool is exempt from default timeouts (e.g., interactive or heavy task).
}

// toolEntry stores a tool's definition, handler, and execution options.
type toolEntry struct {
	declaration *types.ToolDeclaration
	handler     ToolFunc
	options     ToolOptions
}

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	declarations []*types.ToolDeclaration
	entries      map[string]toolEntry
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		declarations: make([]*types.ToolDeclaration, 0),
		entries:      make(map[string]toolEntry),
	}
}

// Register adds a new tool to the registry with default options.
func (r *Registry) Register(def *types.ToolDeclaration, handler ToolFunc) {
	r.RegisterWithOptions(def, handler, ToolOptions{})
}

// RegisterWithOptions adds a new tool to the registry with specific options.
func (r *Registry) RegisterWithOptions(def *types.ToolDeclaration, handler ToolFunc, opts ToolOptions) {
	r.declarations = append(r.declarations, def)
	r.entries[def.Name] = toolEntry{
		declaration: def,
		handler:     handler,
		options:     opts,
	}
}

// GetDeclarations returns the list of function declarations.
func (r *Registry) GetDeclarations() []*types.ToolDeclaration {
	return r.declarations
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) (types.ToolResult, error) {
	entry, ok := r.entries[name]
	if !ok {
		return types.ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	return entry.handler(ctx, args)
}

// IsSerial returns true if the tool is configured for serial execution.
func (r *Registry) IsSerial(name string) bool {
	if entry, ok := r.entries[name]; ok {
		return entry.options.Serial
	}
	return false
}

// IsLongRunning returns true if the tool is configured as long-running (no timeout).
func (r *Registry) IsLongRunning(name string) bool {
	if entry, ok := r.entries[name]; ok {
		return entry.options.LongRunning
	}
	return false
}

// UnmarshalArgs helper converts map[string]interface{} to a target struct.
func UnmarshalArgs(args map[string]interface{}, target interface{}) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}
