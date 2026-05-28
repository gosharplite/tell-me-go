// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"time"
)

// ToolDeclaration represents a function that can be called by the model.
type ToolDeclaration struct {
	// Name is the unique identifier for this tool. It is used by the
	// LLM when requesting a function call and by the Registry for lookup.
	Name string
	// Description is a natural-language explanation of what the tool does.
	// It is included in the LLM's system prompt to guide function selection.
	Description string
	// Parameters defines the JSON Schema for the tool's arguments.
	// When nil, the tool accepts no arguments.
	Parameters *Schema
	// RequiresConsent indicates whether the user must approve the tool
	// invocation before it executes. Consent prompts are displayed by
	// the orchestrator before calling the handler.
	RequiresConsent bool
}

// Schema represents the JSON Schema for a tool's parameters.
// It follows a subset of JSON Schema Draft 2020-12.
type Schema struct {
	// Type is the JSON type: "object", "string", "number", "boolean", "array".
	Type string `json:"type"`
	// Description provides a natural-language explanation of this parameter.
	Description string `json:"description,omitempty"`
	// Properties defines the fields when Type is "object". Keys are
	// parameter names; values are nested schemas.
	Properties map[string]*Schema `json:"properties,omitempty"`
	// Required lists the parameter names that must be provided.
	Required []string `json:"required,omitempty"`
	// Enum restricts values to a fixed set when Type is "string".
	Enum []string `json:"enum,omitempty"`
	// Items defines the element schema when Type is "array".
	Items *Schema `json:"items,omitempty"`
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	// Text is the primary human-readable output of the tool.
	// It is included in the LLM context as the tool's response.
	Text string
	// BinaryData contains multi-modal output (e.g., images, files)
	// produced by the tool.
	BinaryData []BinaryData
	// Error captures a non-terminal error that occurred during execution.
	// The orchestrator may relay this to the LLM for recovery.
	Error error
	// Metadata allows passing structured data back to the orchestrator
	// for post-processing (e.g., file paths, record IDs).
	Metadata map[string]interface{}
}

// BinaryData represents multi-modal content produced by a tool.
type BinaryData struct {
	// MIMEType is the IANA media type (e.g., "image/png", "application/pdf").
	MIMEType string
	// Data is the raw binary content.
	Data []byte
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
//
// The hb chan<- struct{} is a heartbeat channel. When ToolOptions.LivenessThreshold
// is greater than zero, the tool implementation MUST send a heartbeat at intervals
// not exceeding the threshold. Failure to send heartbeats causes ZombieTool to
// declare the execution timed out.
//
// Sending heartbeats is optional when LivenessThreshold is zero. The implementation
// MUST NOT close the channel.
//
// The returned ToolResult may contain an Error field; the orchestrator inspects
// this to determine whether to report the failure to the LLM.
type ToolFunc func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (ToolResult, error)

// ToolOptions defines execution behavior for a tool.
type ToolOptions struct {
	Serial            bool          // If true, the agent waits for this tool to finish before running others.
	LongRunning       bool          // If true, the tool is exempt from default timeouts (e.g., interactive or heavy task).
	LivenessThreshold time.Duration // If > 0, the maximum allowed time between heartbeat signals.
}

// ToolRegistrar defines the interface for adding tools to the registry.
// Registration must happen before the first tool execution; concurrent
// registration and execution is not supported.
type ToolRegistrar interface {
	// Register adds a tool with default options. Returns an error if
	// a tool with the same name is already registered.
	Register(def *ToolDeclaration, handler ToolFunc) error

	// RegisterWithOptions adds a tool with custom execution options
	// (e.g., Serial, LongRunning, LivenessThreshold).
	RegisterWithOptions(def *ToolDeclaration, handler ToolFunc, opts ToolOptions) error

	// RegisterToToolkit adds a tool to a named toolkit. Toolkit
	// membership is used for lazy-loading and scoped declaration
	// requests. The toolkit is created if it does not exist.
	RegisterToToolkit(toolkit string, def *ToolDeclaration, handler ToolFunc) error

	// RegisterToToolkitWithOptions adds a tool to a named toolkit
	// with custom execution options.
	RegisterToToolkitWithOptions(toolkit string, def *ToolDeclaration, handler ToolFunc, opts ToolOptions) error
}

// ToolExecutor defines the interface for executing tools and checking their behavior.
type ToolExecutor interface {
	Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (ToolResult, error)
	IsSerial(name string) bool
	IsLongRunning(name string) bool
	GetOptions(name string) ToolOptions
}

// ToolMetadataProvider defines the interface for listing available tools.
// The declarations returned are used to construct the LLM's function-calling
// schema in each request.
type ToolMetadataProvider interface {
	// GetDeclarations returns all registered tool declarations.
	GetDeclarations() []*ToolDeclaration

	// GetCoreDeclarations returns declarations for tools that are
	// always available, regardless of which toolkits are loaded.
	GetCoreDeclarations() []*ToolDeclaration

	// GetDeclarationsByToolkits returns declarations for all tools
	// belonging to any of the specified toolkits. Duplicates are
	// resolved by name (last wins).
	GetDeclarationsByToolkits(toolkits []string) []*ToolDeclaration

	// ListAvailableToolkits returns the names of all registered toolkits.
	ListAvailableToolkits() []string
}

// Registry defines the interface for the tool registry.
type Registry interface {
	ToolRegistrar
	ToolExecutor
	ToolMetadataProvider
}
