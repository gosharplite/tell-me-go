// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
)

// MCPToolDefinition represents a tool declaration advertised by an MCP server.
type MCPToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
}

// MCPClient defines the domain port for interacting with a Model Context Protocol (MCP) server.
type MCPClient interface {
	// ListTools queries the MCP server for available tools.
	ListTools(ctx context.Context) ([]MCPToolDefinition, error)

	// CallTool executes a tool on the remote MCP server with the provided arguments.
	// A nil args map is normalized to an empty object by implementations — the MCP
	// wire must never carry "arguments": null (strict servers reject it).
	CallTool(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error)

	// Close terminates the client connection and releases any underlying resources.
	Close() error
}
