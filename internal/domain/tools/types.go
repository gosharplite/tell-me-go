// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolDeclaration represents a function that can be called by the model.
type ToolDeclaration struct {
	Name        string
	Description string
	Parameters  *Schema
}

// Schema represents the parameters of a tool.
type Schema struct {
	Type        string
	Description string
	Properties  map[string]*Schema
	Required    []string
	Enum        []string
	Items       *Schema
}

// ToolResult represents the outcome of a tool execution.
type ToolResult struct {
	Text       string
	BinaryData []BinaryData
	Error      error // Optional: captures the error that occurred during execution
}

// BinaryData represents multi-modal content.
type BinaryData struct {
	MIMEType string
	Data     []byte
}

// AgentGateway defines the interface for high-level agent services available to tools.
type AgentGateway interface {
	GenerateImage(ctx context.Context, args map[string]interface{}) (ToolResult, error)
	ReadImage(ctx context.Context, args map[string]interface{}) (ToolResult, error)
}

// UnmarshalArgs helper converts map[string]interface{} to a target struct.
func UnmarshalArgs(args map[string]interface{}, target interface{}) error {
	b, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, target)
}

var ErrNotImplemented = fmt.Errorf("not implemented")
