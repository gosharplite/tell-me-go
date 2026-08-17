// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_mcp "github.com/gosharplite/tell-me-go/internal/infrastructure/mcp"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// mcpFactory builds plugin.MCPServerDependency values from MCP server
// configuration. Bearer-token resolution and client construction are both
// injectable so the factory is unit-testable without spawning `gh` or
// contacting a live MCP endpoint.
type mcpFactory struct {
	logger *slog.Logger

	// tokenResolver resolves a GitHub bearer token (e.g. by executing
	// `gh auth token`). It is invoked only when no explicit server token
	// is configured and the endpoint is GitHub-hosted.
	tokenResolver func() (string, error)

	// newClient constructs the tools.MCPClient adapter for an endpoint.
	newClient func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error)

	// cachedToken memoizes a successfully resolved token so that multiple
	// servers sharing the same fallback do not spawn `gh` repeatedly.
	cachedToken string
}

// newMCPFactory constructs the default MCP dependency factory. The default
// token resolver executes `gh auth token`; the default client constructor is
// the streamable-HTTP MCP adapter.
func newMCPFactory(logger *slog.Logger) *mcpFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &mcpFactory{
		logger: logger,
		tokenResolver: func() (string, error) {
			out, err := exec.Command("gh", "auth", "token").Output()
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(out)), nil
		},
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
			return infra_mcp.NewClient(endpoint, token, timeout)
		},
	}
}

// Build constructs the MCP client dependencies for every configured server.
// A server whose token cannot be resolved, or whose client cannot be
// constructed, is skipped with a warning rather than failing startup.
func (f *mcpFactory) Build(servers map[string]config.MCPServerConfig) map[string]plugin.MCPServerDependency {
	deps := make(map[string]plugin.MCPServerDependency, len(servers))

	for name, serverCfg := range servers {
		token := serverCfg.Token
		if token == "" && requiresMCPServerAuth(serverCfg.URL) {
			token = f.resolveToken()
		}

		if token == "" && requiresMCPServerAuth(serverCfg.URL) {
			f.logger.Warn("mcp_token_resolution_skipped", "server", name, "url", serverCfg.URL)
			continue
		}

		client, err := f.newClient(serverCfg.URL, token, serverCfg.EffectiveTimeout())
		if err != nil {
			f.logger.Warn("mcp_client_init_failed", "server", name, "error", err)
			continue
		}

		deps[name] = plugin.MCPServerDependency{
			Client:          client,
			RequiresConsent: serverCfg.EffectiveRequiresConsent(),
			Serial:          serverCfg.EffectiveSerial(),
		}
	}

	return deps
}

// resolveToken resolves a bearer token using the cached value, the injected
// `gh auth token` resolver, and finally the GITHUB_TOKEN environment variable.
// A successfully resolved token is memoized for subsequent servers.
func (f *mcpFactory) resolveToken() string {
	if f.cachedToken != "" {
		return f.cachedToken
	}

	if f.tokenResolver != nil {
		if token, err := f.tokenResolver(); err == nil && token != "" {
			f.cachedToken = token
			return token
		}
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		f.cachedToken = token
		return token
	}

	return ""
}

// requiresMCPServerAuth reports whether an MCP endpoint requires a bearer
// token. GitHub-hosted remote MCP endpoints (github.com, api.github.com,
// api.githubcopilot.com, and other *.github* hosts) require authentication;
// self-hosted or local endpoints do not.
func requiresMCPServerAuth(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return strings.Contains(strings.ToLower(u.Hostname()), "github")
}
