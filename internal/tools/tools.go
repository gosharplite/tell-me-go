// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"fmt"
	"google.golang.org/genai"
)

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(args map[string]interface{}) (string, error)

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	declarations []*genai.FunctionDeclaration
	handlers     map[string]ToolFunc
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		declarations: make([]*genai.FunctionDeclaration, 0),
		handlers:     make(map[string]ToolFunc),
	}
}

// Register adds a new tool to the registry.
func (r *Registry) Register(def *genai.FunctionDeclaration, handler ToolFunc) {
	r.declarations = append(r.declarations, def)
	r.handlers[def.Name] = handler
}

// GetDeclarations returns the list of function declarations.
func (r *Registry) GetDeclarations() []*genai.FunctionDeclaration {
	return r.declarations
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(name string, args map[string]interface{}) (string, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return handler(args)
}

// ToToolSDK converts declarations into the format expected by the GenAI SDK.
func (r *Registry) ToToolSDK() []*genai.Tool {
	if len(r.declarations) == 0 {
		return nil
	}
	return []*genai.Tool{
		{
			FunctionDeclarations: r.declarations,
		},
	}
}
