// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
)

// ToolDeclaration represents a function that can be called by the model.
type ToolDeclaration struct {
	Name            string
	Description     string
	Parameters      *Schema
	RequiresConsent bool
}

// Schema represents the parameters of a tool.
type Schema struct {
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	Properties  map[string]*Schema `json:"properties,omitempty"`
	Required    []string           `json:"required,omitempty"`
	Enum        []string           `json:"enum,omitempty"`
	Items       *Schema            `json:"items,omitempty"`
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	Text       string
	BinaryData []BinaryData
	Error      error                  // Optional: captures the error that occurred during execution
	Metadata   map[string]interface{} // Metadata allows passing structured data back to the orchestrator
}

// BinaryData represents multi-modal content.
type BinaryData struct {
	MIMEType string
	Data     []byte
}

// UnmarshalArgs helper converts map[string]interface{} to a target struct.
func UnmarshalArgs(args map[string]interface{}, target interface{}) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(ctx context.Context, args map[string]interface{}) (ToolResult, error)

// ToolOptions defines execution behavior for a tool.
type ToolOptions struct {
	Serial      bool // If true, the agent waits for this tool to finish before running others.
	LongRunning bool // If true, the tool is exempt from default timeouts (e.g., interactive or heavy task).
}

// ToolRegistrar defines the interface for adding tools to the registry.
type ToolRegistrar interface {
	Register(def *ToolDeclaration, handler ToolFunc) error
	RegisterWithOptions(def *ToolDeclaration, handler ToolFunc, opts ToolOptions) error
}

// ToolExecutor defines the interface for executing tools and checking their behavior.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error)
	IsSerial(name string) bool
	IsLongRunning(name string) bool
}

// ToolMetadataProvider defines the interface for listing available tools.
type ToolMetadataProvider interface {
	GetDeclarations() []*ToolDeclaration
}

// IToolRegistry defines the interface for the tool registry.
type IToolRegistry interface {
	ToolRegistrar
	ToolExecutor
	ToolMetadataProvider
}
