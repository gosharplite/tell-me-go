// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package mcp implements the MCP integration plugin. It dynamically discovers
// tools from each configured MCP server (via the tools.MCPClient domain port)
// and registers them into the tool registry under a deterministic, namespaced
// name. The package interacts with MCP exclusively through the domain port and
// plugin.MCPServerDependency — it never imports internal/infrastructure.
package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sort"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// mcpPlugin implements plugin.Plugin for the MCP integration. A single plugin
// discovers and registers tools from every configured MCP server.
type mcpPlugin struct{}

// NewPlugin returns a new MCP plugin for the global registry.
func NewPlugin() plugin.Plugin { return &mcpPlugin{} }

func (mcpPlugin) Name() string { return "mcp" }

func (mcpPlugin) Register(r tools.Registry, deps plugin.PluginDependencies) error {
	if len(deps.MCPClients) == 0 {
		return nil
	}

	// Deterministic registration order so tool discovery is reproducible
	// across runs regardless of map iteration order.
	serverNames := make([]string, 0, len(deps.MCPClients))
	for name := range deps.MCPClients {
		serverNames = append(serverNames, name)
	}
	sort.Strings(serverNames)

	for _, serverName := range serverNames {
		dep := deps.MCPClients[serverName]

		toolsList, err := dep.Client.ListTools(context.Background())
		if err != nil {
			// Non-fatal: a single server failing discovery must not abort
			// registration of other MCP servers or core tools.
			slog.Warn("mcp_server_discovery_failed", "server", serverName, "error", err)
			continue
		}

		for _, t := range toolsList {
			namespacedName := FormatToolName(serverName, t.Name)
			paramSchema := ConvertSchema(t.InputSchema, namespacedName)

			decl := &tools.ToolDeclaration{
				Name:            namespacedName,
				Description:     t.Description,
				Parameters:      paramSchema,
				RequiresConsent: dep.RequiresConsent,
			}

			handler := func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				return dep.Client.CallTool(ctx, t.Name, args)
			}

			opts := tools.ToolOptions{
				Serial:            dep.Serial,
				LongRunning:       true,
				LivenessThreshold: 0,
			}
			if err := r.RegisterWithOptions(decl, handler, opts); err != nil {
				return fmt.Errorf("mcp register tool %q: %w", namespacedName, err)
			}
		}
	}

	return nil
}

// FormatToolName builds a deterministic, namespaced tool name from an MCP
// server name and a tool name. The result is "mcp_<server>_<tool>"; when that
// exceeds 64 bytes, the tool name is truncated (by runes, capped at 40) and a
// stable 8-hex-char SHA-256 prefix of the full tool name is appended to
// preserve uniqueness.
func FormatToolName(server, tool string) string {
	base := fmt.Sprintf("mcp_%s_%s", server, tool)
	if len(base) <= 64 {
		return base
	}

	sum := sha256.Sum256([]byte(tool))
	hash8 := hex.EncodeToString(sum[:])[:8]

	// Budget for the truncated tool prefix: the fixed segments are
	// "mcp_" (4) + server + "_" (1) + "_" (1) + hash8 (8).
	maxPrefixLen := 64 - len("mcp_") - len(server) - len("_") - len("_") - len(hash8)
	if maxPrefixLen > 40 {
		maxPrefixLen = 40
	}
	if maxPrefixLen < 0 {
		maxPrefixLen = 0
	}

	prefix := truncateRunes(tool, maxPrefixLen)
	return fmt.Sprintf("mcp_%s_%s_%s", server, prefix, hash8)
}

// truncateRunes truncates s to at most maxRunes runes. A non-positive limit
// yields an empty string.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

func init() {
	_ = plugin.Register(NewPlugin()) //nolint:errcheck // init() cannot return errors; duplicate names caught in tests
}
