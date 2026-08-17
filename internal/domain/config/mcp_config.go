// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// mcpServerKeyRegex enforces valid server key format (lowercase alphanumeric and hyphens).
var mcpServerKeyRegex = regexp.MustCompile(`^[a-z0-9-]+$`)

// DefaultMCPTimeoutSeconds is the default timeout for MCP tool calls (5 minutes).
const DefaultMCPTimeoutSeconds = 300

// MCPServerConfig defines configuration for an external Model Context Protocol server.
type MCPServerConfig struct {
	URL             string `yaml:"URL"`
	Token           string `yaml:"TOKEN"`
	RequiresConsent *bool  `yaml:"REQUIRES_CONSENT"`
	Timeout         int    `yaml:"TIMEOUT"` // Seconds (0 = default 300s)
}

// IsReadOnly reports whether the server URL targets a read-only endpoint.
// A URL whose path ends with a "/readonly" segment is considered read-only.
func (c *MCPServerConfig) IsReadOnly() bool {
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
	return !c.IsReadOnly()
}

// EffectiveSerial reports whether tool calls against this server must execute
// serially. "/readonly" endpoints may execute concurrently (false); mutating
// endpoints execute serially (true).
func (c *MCPServerConfig) EffectiveSerial() bool {
	return !c.IsReadOnly()
}

// EffectiveTimeout resolves the per-operation timeout for this server. A
// positive Timeout wins; zero falls back to DefaultMCPTimeoutSeconds.
func (c *MCPServerConfig) EffectiveTimeout() time.Duration {
	if c.Timeout > 0 {
		return time.Duration(c.Timeout) * time.Second
	}
	return DefaultMCPTimeoutSeconds * time.Second
}

// validate reports hard configuration errors for a single MCP server.
func (c *MCPServerConfig) validate(name string) error {
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("MCP_SERVERS.%s.URL must not be empty", name)
	}
	if c.Timeout < 0 {
		return fmt.Errorf("MCP_SERVERS.%s.TIMEOUT must be >= 0, got %d", name, c.Timeout)
	}
	return nil
}
