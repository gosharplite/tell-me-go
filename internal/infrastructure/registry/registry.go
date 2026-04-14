// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package registry maintains a mapping between function names and their Go implementations.
package registry

import (
	"context"
	"fmt"
	"sort"
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
	toolkitMap   map[string][]*tools.ToolDeclaration
}

// New initializes an empty tool registry.
func New() tools.Registry {
	return &registry{
		declarations: make([]*tools.ToolDeclaration, 0),
		entries:      make(map[string]toolEntry),
		toolkitMap:   make(map[string][]*tools.ToolDeclaration),
	}
}

// Register adds a new tool to the registry with default options and core toolkit.
func (r *registry) Register(def *tools.ToolDeclaration, handler toolFunc) error {
	return r.RegisterToToolkit("core", def, handler)
}

// RegisterWithOptions adds a new tool to the registry with specific options and core toolkit.
func (r *registry) RegisterWithOptions(def *tools.ToolDeclaration, handler toolFunc, opts ToolOptions) error {
	return r.RegisterToToolkitWithOptions("core", def, handler, opts)
}

// RegisterToToolkit adds a tool to a specific toolkit.
func (r *registry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler toolFunc) error {
	return r.RegisterToToolkitWithOptions(toolkit, def, handler, ToolOptions{})
}

// RegisterToToolkitWithOptions adds a tool to a specific toolkit with options.
func (r *registry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler toolFunc, opts ToolOptions) error {
	var err error
	toolkit, err = validateRegistration(toolkit, def)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for existing tool to avoid duplicates in entries
	if _, exists := r.entries[def.Name]; exists {
		r.updateExistingRegistration(toolkit, def, handler, opts)
		return nil
	}

	r.declarations = append(r.declarations, def)
	r.entries[def.Name] = toolEntry{
		Declaration: def,
		Handler:     handler,
		Options:     opts,
	}
	r.toolkitMap[toolkit] = append(r.toolkitMap[toolkit], def)
	return nil
}

func validateRegistration(toolkit string, def *tools.ToolDeclaration) (string, error) {
	if def.Name == "" {
		return "", fmt.Errorf("cannot register tool with empty name")
	}
	if toolkit == "" {
		toolkit = "core"
	}
	return toolkit, nil
}

func (r *registry) updateExistingRegistration(toolkit string, def *tools.ToolDeclaration, handler toolFunc, opts ToolOptions) {
	// Update existing entry
	entry := r.entries[def.Name]
	entry.Declaration = def
	entry.Handler = handler
	entry.Options = opts
	r.entries[def.Name] = entry

	r.updateDeclarationsList(def)
	r.updateToolkitMap(toolkit, def)
}

func (r *registry) updateDeclarationsList(def *tools.ToolDeclaration) {
	for i, d := range r.declarations {
		if d.Name == def.Name {
			r.declarations[i] = def
			break
		}
	}
}

func (r *registry) updateToolkitMap(toolkit string, def *tools.ToolDeclaration) {
	for tk, toolsList := range r.toolkitMap {
		for i, d := range toolsList {
			if d.Name == def.Name {
				r.toolkitMap[tk][i] = def
			}
		}
	}

	// If it's not in the target toolkit, add it
	foundInToolkit := false
	for _, d := range r.toolkitMap[toolkit] {
		if d.Name == def.Name {
			foundInToolkit = true
			break
		}
	}
	if !foundInToolkit {
		r.toolkitMap[toolkit] = append(r.toolkitMap[toolkit], def)
	}
}

// GetDeclarations returns the list of all function declarations.
func (r *registry) GetDeclarations() []*tools.ToolDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()
	// Return a copy to ensure thread safety for the caller
	res := make([]*tools.ToolDeclaration, len(r.declarations))
	copy(res, r.declarations)
	return res
}

// GetCoreDeclarations returns all tool declarations belonging to the "core" toolkit.
func (r *registry) GetCoreDeclarations() []*tools.ToolDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	coreTools := r.toolkitMap["core"]
	res := make([]*tools.ToolDeclaration, len(coreTools))
	copy(res, coreTools)
	return res
}

// GetDeclarationsByToolkits returns core tools plus tools from requested toolkits, deduplicated.
func (r *registry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use a map for deduplication by name
	dedup := make(map[string]*tools.ToolDeclaration)

	// Always add core
	for _, d := range r.toolkitMap["core"] {
		dedup[d.Name] = d
	}

	// Add requested toolkits
	for _, tk := range toolkits {
		if tk == "core" {
			continue
		}
		for _, d := range r.toolkitMap[tk] {
			dedup[d.Name] = d
		}
	}

	res := make([]*tools.ToolDeclaration, 0, len(dedup))
	for _, d := range dedup {
		res = append(res, d)
	}

	// Sort by Name for deterministic output
	sort.Slice(res, func(i, j int) bool {
		return res[i].Name < res[j].Name
	})

	return res
}

// ListAvailableToolkits returns all registered toolkit names.
func (r *registry) ListAvailableToolkits() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	toolkits := make([]string, 0, len(r.toolkitMap))
	for tk := range r.toolkitMap {
		toolkits = append(toolkits, tk)
	}
	return toolkits
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *registry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	r.mu.RLock()
	entry, ok := r.entries[name]
	r.mu.RUnlock()

	if !ok {
		return tools.ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	res, err := entry.Handler(ctx, args, hb)
	if err != nil {
		return res, fmt.Errorf("tool execution failed: %s: %w", name, err)
	}
	return res, nil
}

// GetOptions returns the options associated with a tool.
func (r *registry) GetOptions(name string) tools.ToolOptions {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.entries[name]; ok {
		return entry.Options
	}
	return tools.ToolOptions{}
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
