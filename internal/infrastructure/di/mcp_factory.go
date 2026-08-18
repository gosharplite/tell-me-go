// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_mcp "github.com/gosharplite/tell-me-go/internal/infrastructure/mcp"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// mcpFactory builds plugin.MCPServerDependency values from MCP server
// configuration. Credential resolution and client construction are both
// injectable so the factory is unit-testable without spawning `gh` or
// contacting a live MCP endpoint.
type mcpFactory struct {
	logger *slog.Logger

	// tokenResolver resolves a GitHub bearer token (e.g. by executing
	// `gh auth token`). It is invoked only under the "gh" auth mode (no
	// explicit token) or the "auto" auth mode (GitHub-hosted, no explicit
	// token).
	tokenResolver func() (string, error)

	// newClient constructs the tools.MCPClient adapter for an endpoint.
	newClient func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error)

	// cachedToken memoizes a successfully resolved token so that multiple
	// servers sharing the same fallback do not spawn `gh` repeatedly.
	cachedToken string

	// mu protects clients. Build may be invoked concurrently with Close
	// during session teardown, so client tracking is synchronized.
	mu sync.Mutex

	// clients tracks every successfully constructed MCP client so that
	// session teardown can close them cleanly.
	clients []tools.MCPClient
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			if username != "" {
				return infra_mcp.NewClient(endpoint, token, timeout, infra_mcp.WithBasicAuth(username))
			}
			return infra_mcp.NewClient(endpoint, token, timeout)
		},
	}
}

// Build constructs the MCP client dependencies for every configured server.
// A server whose credentials cannot be resolved, or whose client cannot be
// constructed, is skipped with a warning rather than failing startup.
func (f *mcpFactory) Build(servers map[string]config.MCPServerConfig) map[string]plugin.MCPServerDependency {
	deps := make(map[string]plugin.MCPServerDependency, len(servers))

	for name, serverCfg := range servers {
		username, token, ok := f.resolveServerToken(name, serverCfg)
		if !ok {
			continue
		}

		client, err := f.newClient(serverCfg.URL, username, token, serverCfg.EffectiveTimeout())
		if err != nil {
			f.logger.Warn("mcp_client_init_failed", "server", name, "error", err)
			continue
		}

		f.mu.Lock()
		f.clients = append(f.clients, client)
		f.mu.Unlock()

		deps[name] = plugin.MCPServerDependency{
			Client:          client,
			RequiresConsent: serverCfg.EffectiveRequiresConsent(),
			Serial:          serverCfg.EffectiveSerial(),
		}
	}

	return deps
}

// Close terminates every MCP client constructed by this factory, joining any
// errors with errors.Join. It is safe to call concurrently with Build.
func (f *mcpFactory) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()

	var errs []error
	for _, client := range f.clients {
		if err := client.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// resolveServerToken resolves the credentials (username, token) for a single
// server from its effective auth mode. It returns ok=false when the server
// must be skipped (token resolution failed under an auth mode that requires
// it).
func (f *mcpFactory) resolveServerToken(name string, serverCfg config.MCPServerConfig) (username, token string, ok bool) {
	auth := serverCfg.EffectiveAuth()

	switch auth {
	case config.MCPAuthNone:
		return "", "", true

	case config.MCPAuthBearer:
		return "", serverCfg.Token, true

	case config.MCPAuthBasic:
		return serverCfg.Username, serverCfg.Token, true

	case config.MCPAuthGH:
		if serverCfg.Token != "" {
			return "", serverCfg.Token, true
		}
		token, ok := f.resolveTokenOrSkip(name, serverCfg.URL)
		return "", token, ok

	case config.MCPAuthAuto:
		if serverCfg.Token != "" {
			return "", serverCfg.Token, true
		}
		if !isGitHubHostname(serverCfg.URL) {
			return "", "", true
		}
		token, ok := f.resolveTokenOrSkip(name, serverCfg.URL)
		return "", token, ok

	default:
		// Unknown modes are rejected by config validation before Build runs;
		// treat defensively as "none".
		return "", "", true
	}
}

// resolveTokenOrSkip resolves a bearer token and returns ok=false (after
// emitting a warning) when resolution fails.
func (f *mcpFactory) resolveTokenOrSkip(name, rawURL string) (string, bool) {
	token := f.resolveToken()
	if token == "" {
		f.logger.Warn("mcp_token_resolution_skipped", "server", name, "url", rawURL)
		return "", false
	}
	return token, true
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

// isGitHubHostname reports whether an MCP endpoint host is GitHub-hosted
// (github.com, api.github.com, api.githubcopilot.com, and other *.github*
// hosts). Used only by the "auto" auth mode to choose between GitHub-token
// resolution and no authentication.
func isGitHubHostname(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return false
	}
	return strings.Contains(strings.ToLower(u.Hostname()), "github")
}
