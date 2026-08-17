// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// mcpServerKeyRegex enforces the valid server key format: 1 to 24 lowercase
// alphanumeric characters and hyphens. The 24-character upper bound is a
// hard guarantee that every generated tool name stays within 64 bytes (see
// formatToolName): the fixed segments "mcp_" (4) + server (≤24) + "_" (1) +
// hash8 (8) leave a tool-prefix budget that shrinks as the server name grows,
// so the total never exceeds 64 bytes.
var mcpServerKeyRegex = regexp.MustCompile(`^[a-z0-9-]{1,24}$`)

// defaultMCPTimeoutSeconds is the default timeout for MCP tool calls (5 minutes).
const defaultMCPTimeoutSeconds = 300

// MCP auth mode constants. The effective auth mode of a server determines how
// its bearer token is resolved at client-construction time.
const (
	MCPAuthAuto   = "auto"   // explicit token wins; else GitHub-hosted → gh; else none
	MCPAuthGH     = "gh"     // resolve via gh auth token → GITHUB_TOKEN
	MCPAuthBearer = "bearer" // use the explicit TOKEN
	MCPAuthNone   = "none"   // no authentication
)

// MCPServerConfig defines configuration for an external Model Context Protocol server.
type MCPServerConfig struct {
	URL             string `yaml:"URL"`
	Token           string `yaml:"TOKEN"`
	Auth            string `yaml:"AUTH"` // "auto", "gh", "bearer", "none" (default: "auto")
	RequiresConsent *bool  `yaml:"REQUIRES_CONSENT"`
	Timeout         int    `yaml:"TIMEOUT"` // Seconds (0 = default 300s)
}

// isReadOnly reports whether the server URL targets a read-only endpoint.
// A URL whose path ends with a "/readonly" segment is considered read-only.
func (c *MCPServerConfig) isReadOnly() bool {
	return strings.HasSuffix(strings.TrimRight(c.URL, "/"), "/readonly")
}

// EffectiveRequiresConsent resolves whether tool calls against this server
// require user consent. An explicit REQUIRES_CONSENT wins; otherwise
// "/readonly" endpoints default to false and mutating endpoints default to
// true.
func (c *MCPServerConfig) EffectiveRequiresConsent() bool {
	if c.RequiresConsent != nil {
		return *c.RequiresConsent
	}
	return !c.isReadOnly()
}

// EffectiveSerial reports whether tool calls against this server must execute
// serially. "/readonly" endpoints may execute concurrently (false); mutating
// endpoints execute serially (true).
func (c *MCPServerConfig) EffectiveSerial() bool {
	return !c.isReadOnly()
}

// EffectiveAuth returns the effective auth mode for this server, normalizing
// to lowercase and trimming surrounding whitespace. An empty value defaults to
// MCPAuthAuto.
func (c *MCPServerConfig) EffectiveAuth() string {
	if c.Auth == "" {
		return MCPAuthAuto
	}
	return strings.ToLower(strings.TrimSpace(c.Auth))
}

// EffectiveTimeout resolves the per-operation timeout for this server. A
// positive Timeout wins; zero falls back to defaultMCPTimeoutSeconds.
func (c *MCPServerConfig) EffectiveTimeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout) * time.Second
	}
	return defaultMCPTimeoutSeconds * time.Second
}

// validate reports hard configuration errors for a single MCP server.
func (c *MCPServerConfig) validate(name string) error {
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("MCP_SERVERS.%s.URL must not be empty", name)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("MCP_SERVERS.%s.TIMEOUT must be >= 0, got %d", name, c.Timeout)
	}

	auth := c.EffectiveAuth()
	switch auth {
	case MCPAuthAuto, MCPAuthGH, MCPAuthBearer, MCPAuthNone:
		// valid
	default:
		return fmt.Errorf("MCP_SERVERS.%s.AUTH must be one of %q, %q, %q, %q, got %q",
			name, MCPAuthAuto, MCPAuthGH, MCPAuthBearer, MCPAuthNone, auth)
	}

	if auth == MCPAuthBearer && strings.TrimSpace(c.Token) == "" {
		return fmt.Errorf("MCP_SERVERS.%s.TOKEN must not be empty when AUTH is bearer", name)
	}

	return nil
}
