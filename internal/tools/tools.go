// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"fmt"
)

// Definition represents the JSON schema definition for a tool/function.
type Definition struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  interface{} `json:"parameters"`
}

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(args map[string]interface{}) (string, error)

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	definitions []Definition
	handlers    map[string]ToolFunc
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		definitions: make([]Definition, 0),
		handlers:    make(map[string]ToolFunc),
	}
}

// Register adds a new tool to the registry.
func (r *Registry) Register(def Definition, handler ToolFunc) {
	r.definitions = append(r.definitions, def)
	r.handlers[def.Name] = handler
}

// GetDefinitions returns the list of tool definitions for the API payload.
func (r *Registry) GetDefinitions() []Definition {
	return r.definitions
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(name string, args map[string]interface{}) (string, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return handler(args)
}

// ToToolJSON converts definitions into the format expected by the Gemini API.
func (r *Registry) ToToolJSON() interface{} {
	if len(r.definitions) == 0 {
		return nil
	}
	return []map[string]interface{}{
		{
			"functionDeclarations": r.definitions,
		},
	}
}
