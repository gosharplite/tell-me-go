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

var (
	ErrNotImplemented  = fmt.Errorf("not implemented")
	ErrToolCircuitOpen = fmt.Errorf("tool circuit breaker is open")
)

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(ctx context.Context, args map[string]interface{}) (ToolResult, error)

type IToolRegistry interface {
	Register(def *ToolDeclaration, handler ToolFunc)
	Execute(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error)
	IsSerial(name string) bool
	IsLongRunning(name string) bool
	GetDeclarations() []*ToolDeclaration
}
