// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"errors"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"

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

	// newClientFor constructs the tools.MCPClient adapter for a single server,
	// dispatching on transport: stdio (COMMAND) vs remote Streamable HTTP (URL).
	// It is injectable so the factory is unit-testable without spawning a
	// process or contacting a live endpoint.
	newClientFor func(serverCfg config.MCPServerConfig) (tools.MCPClient, error)

	// cachedToken memoizes a successfully resolved token so that multiple
	// servers sharing the same fallback do not spawn `gh` repeatedly.
	// Written and read only during Build's phase-1 pre-pass —
	// resolveEntries (single-threaded); phase-2 goroutines never call resolveToken.
	cachedToken string

	// mu protects clients. Build may be invoked concurrently with Close
	// during session teardown, so client tracking is synchronized.
	mu sync.Mutex

	// clients tracks every successfully constructed MCP client so that
	// session teardown can close them cleanly.
	clients []tools.MCPClient

	// byName stashes every successfully constructed MCP client keyed by its
	// configured server name so the client survives tool registration (the
	// plugin.MCPServerDependency map produced by Build is not retained). It
	// is guarded by mu, like clients.
	byName map[string]tools.MCPClient
}

// newMCPFactory constructs the default MCP dependency factory. The default
// token resolver executes `gh auth token`; the default client constructor
// dispatches on transport — local stdio child processes via StdioClient, and
// remote Streamable HTTP endpoints via the HTTP MCP adapter.
func newMCPFactory(logger *slog.Logger) *mcpFactory {
	if logger == nil {
		logger = slog.Default()
	}
	return &mcpFactory{
		logger: logger,
		byName: make(map[string]tools.MCPClient),
		tokenResolver: func() (string, error) {
			out, err := exec.Command("gh", "auth", "token").Output()
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(string(out)), nil
		},
		newClientFor: func(serverCfg config.MCPServerConfig) (tools.MCPClient, error) {
			if serverCfg.IsStdio() {
				return infra_mcp.NewStdioClient(serverCfg, logger)
			}
			if serverCfg.Username != "" {
				return infra_mcp.NewClient(serverCfg.URL, serverCfg.Token, serverCfg.EffectiveTimeout(), infra_mcp.WithBasicAuth(serverCfg.Username))
			}
			return infra_mcp.NewClient(serverCfg.URL, serverCfg.Token, serverCfg.EffectiveTimeout())
		},
	}
}

// mcpEntry is one phase-1 resolution outcome: the server name, its
// (credential-stamped) config copy, and whether it survived resolution.
type mcpEntry struct {
	name string
	cfg  config.MCPServerConfig // HTTP: Token/Username stamped; stdio: unchanged
	ok   bool
}

// mcpBuildResult is one phase-2 construction outcome for a single server.
type mcpBuildResult struct {
	name   string
	cfg    config.MCPServerConfig
	client tools.MCPClient
	err    error
}

// Build constructs the MCP client dependencies for every configured server.
// Construction is two-phase: a sequential token pre-pass resolves and stamps
// credentials into per-server copies (single-flight gh memoization), then
// per-server construction runs concurrently. A server whose credentials
// cannot be resolved, or whose client cannot be constructed, is skipped with
// a warning rather than failing startup.
func (f *mcpFactory) Build(servers map[string]config.MCPServerConfig) map[string]plugin.MCPServerDependency {
	deps := make(map[string]plugin.MCPServerDependency, len(servers))
	if len(servers) == 0 {
		return deps
	}

	// Deterministic iteration order (map iteration is not).
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)

	entries := f.resolveEntries(names, servers)

	// Phase 2 — CONCURRENT construction, mirroring the plugin's own pattern
	// (internal/tools/integrations/mcp/plugin.go:61-89). Per-server
	// spawn+handshake is bounded by EffectiveTimeout inside the constructor.
	// Goroutines write only to their own result slot — no shared writes, no
	// locks in the hot path. Failures warn+skip per server; no overall bound.
	results := make([]mcpBuildResult, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		wg.Add(1)
		go func(i int, e mcpEntry) {
			defer wg.Done()
			client, err := f.newClientFor(e.cfg)
			results[i] = mcpBuildResult{name: e.name, cfg: e.cfg, client: client, err: err}
		}(i, e)
	}
	wg.Wait()

	f.registerClientResults(results, deps)
	return deps
}

// resolveEntries is Build's phase 1 — SEQUENTIAL token pre-pass. Stdio
// servers short-circuit first: gh/auto under COMMAND never resolve, never
// warn, never invoke the resolver. HTTP servers resolve via
// resolveServerToken (single-flight gh memoization via f.cachedToken —
// written and read only in this phase, which is single-threaded). Resolved
// credentials are stamped into a per-server copy so phase 2 goroutines
// never touch shared mutable state.
func (f *mcpFactory) resolveEntries(names []string, servers map[string]config.MCPServerConfig) []mcpEntry {
	entries := make([]mcpEntry, 0, len(names))
	for _, name := range names {
		serverCfg := servers[name]
		if serverCfg.IsStdio() {
			entries = append(entries, mcpEntry{name: name, cfg: serverCfg, ok: true})
			continue
		}
		username, token, ok := f.resolveServerToken(name, serverCfg)
		if !ok {
			continue // resolver already warned (mcp_token_resolution_skipped)
		}
		resolved := serverCfg
		resolved.Username = username
		resolved.Token = token
		entries = append(entries, mcpEntry{name: name, cfg: resolved, ok: true})
	}
	return entries
}

// registerClientResults is Build's phase-2 collation — merge deterministically
// (sorted by name) so warn order and registration order are stable regardless
// of goroutine completion order.
func (f *mcpFactory) registerClientResults(results []mcpBuildResult, deps map[string]plugin.MCPServerDependency) {
	for _, res := range results {
		if res.err != nil {
			f.logger.Warn("mcp_client_init_failed", "server", res.name, "error", res.err)
			continue
		}
		f.mu.Lock()
		if f.byName == nil {
			// Zero-value / struct-literal factories (tests) have a nil
			// byName; map assignment would panic, so initialize lazily.
			f.byName = make(map[string]tools.MCPClient)
		}
		f.clients = append(f.clients, res.client)
		f.byName[res.name] = res.client
		f.mu.Unlock()
		deps[res.name] = plugin.MCPServerDependency{
			Client:          res.client,
			RequiresConsent: res.cfg.EffectiveRequiresConsent(),
			Serial:          res.cfg.EffectiveSerial(),
		}
	}
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

// Client returns the stashed client for a server name, or (nil, false)
// when the server was skipped during Build or never configured. Safe to
// call concurrently with Build/Close.
func (f *mcpFactory) Client(name string) (tools.MCPClient, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.byName[name]
	return c, ok
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
