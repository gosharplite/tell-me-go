// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package registry maintains a mapping between function names and their Go implementations.
package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ToolFunc is the signature for Go functions that can be called by the model.
type toolFunc = tools.ToolFunc

// ToolOptions defines execution behavior for a tool.
type ToolOptions = tools.ToolOptions

// ToolEntry stores a tool's definition, handler, and execution options.
type toolEntry struct {
	Declaration *tools.ToolDeclaration
	Handler     toolFunc
	Options     ToolOptions
}

// registry maintains a mapping between function names and their Go implementations.
type registry struct {
	mu           sync.RWMutex
	declarations []*tools.ToolDeclaration
	entries      map[string]toolEntry
}

// New initializes an empty tool registry.
func New() tools.IToolRegistry {
	return &registry{
		declarations: make([]*tools.ToolDeclaration, 0),
		entries:      make(map[string]toolEntry),
	}
}

// Register adds a new tool to the registry with default options.
func (r *registry) Register(def *tools.ToolDeclaration, handler toolFunc) {
	r.RegisterWithOptions(def, handler, ToolOptions{})
}

// RegisterWithOptions adds a new tool to the registry with specific options.
func (r *registry) RegisterWithOptions(def *tools.ToolDeclaration, handler toolFunc, opts ToolOptions) {
	if def.Name == "" {
		panic("cannot register tool with empty name")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for existing tool to avoid duplicates in declarations
	if entry, exists := r.entries[def.Name]; exists {
		// Update existing entry
		entry.Declaration = def
		entry.Handler = handler
		entry.Options = opts
		r.entries[def.Name] = entry

		// Update declaration in-place if possible, or just skip appending
		for i, d := range r.declarations {
			if d.Name == def.Name {
				r.declarations[i] = def
				break
			}
		}
		return
	}

	r.declarations = append(r.declarations, def)
	r.entries[def.Name] = toolEntry{
		Declaration: def,
		Handler:     handler,
		Options:     opts,
	}
}

// GetDeclarations returns the list of function declarations.
func (r *registry) GetDeclarations() []*tools.ToolDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return a copy to ensure thread safety for the caller
	res := make([]*tools.ToolDeclaration, len(r.declarations))
	copy(res, r.declarations)
	return res
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *registry) Execute(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	r.mu.RLock()
	entry, ok := r.entries[name]
	r.mu.RUnlock()

	if !ok {
		return tools.ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	res, err := entry.Handler(ctx, args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("tool execution failed: %s: %w", name, err)
	}
	return res, nil
}

// IsSerial returns true if the tool is configured for serial execution.
func (r *registry) IsSerial(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.entries[name]; ok {
		return entry.Options.Serial
	}
	return false
}

// IsLongRunning returns true if the tool is configured as long-running (no timeout).
func (r *registry) IsLongRunning(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.entries[name]; ok {
		return entry.Options.LongRunning
	}
	return false
}
