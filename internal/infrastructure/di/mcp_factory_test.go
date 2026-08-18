// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// fakeMCPClient is a minimal tools.MCPClient double used to assert that a
// dependency's Client field was populated without contacting a live server.
type fakeMCPClient struct{}

func (f *fakeMCPClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	return nil, nil
}

func (f *fakeMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (f *fakeMCPClient) Close() error { return nil }

// closeCountingMCPClient is a tools.MCPClient double that records Close calls
// so teardown wiring can be asserted without contacting a live server.
type closeCountingMCPClient struct {
	closeCalls int
	closeErr   error
}

func (c *closeCountingMCPClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	return nil, nil
}

func (c *closeCountingMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (c *closeCountingMCPClient) Close() error {
	c.closeCalls++
	return c.closeErr
}

// TestMCPFactory_Close verifies that Close iterates every tracked client,
// calls Close on each, and joins any errors with errors.Join.
func TestMCPFactory_Close(t *testing.T) {
	clientA := &closeCountingMCPClient{}
	clientB := &closeCountingMCPClient{closeErr: errors.New("boom")}
	clientC := &closeCountingMCPClient{}

	f := &mcpFactory{
		logger:  slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		clients: []tools.MCPClient{clientA, clientB, clientC},
	}

	err := f.Close()
	if err == nil {
		t.Fatal("Close() error = nil, want joined error")
	}
	if !errors.Is(err, clientB.closeErr) {
		t.Errorf("Close() error = %v, want it to wrap %v", err, clientB.closeErr)
	}
	for i, c := range []*closeCountingMCPClient{clientA, clientB, clientC} {
		if c.closeCalls != 1 {
			t.Errorf("client[%d].Close() calls = %d, want 1", i, c.closeCalls)
		}
	}
}

func TestMCPFactory_ExplicitToken(t *testing.T) {
	resolverCalls := 0
	var capturedToken string

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			capturedToken = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://api.githubcopilot.com/mcp/", Token: "explicit-token"},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (explicit token must win)", resolverCalls)
	}
	dep, ok := deps["github"]
	if !ok {
		t.Fatal("expected 'github' dependency")
	}
	if dep.Client == nil {
		t.Error("expected non-nil MCP client")
	}
	if capturedToken != "explicit-token" {
		t.Errorf("client token = %q, want %q", capturedToken, "explicit-token")
	}
}

func TestMCPFactory_GhFallbackAndCaching(t *testing.T) {
	resolverCalls := 0
	var mu sync.Mutex
	tokens := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			mu.Lock()
			tokens[cfg.URL] = cfg.Token
			mu.Unlock()
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github":         {URL: "https://github.com/org/repo/mcp"},
		"github-copilot": {URL: "https://api.githubcopilot.com/mcp/"},
	})

	if resolverCalls != 1 {
		t.Errorf("token resolver called %d times, want exactly 1 (cached across servers)", resolverCalls)
	}
	if len(deps) != 2 {
		t.Fatalf("deps count = %d, want 2", len(deps))
	}
	if len(tokens) != 2 {
		t.Fatalf("client constructor called for %d servers, want 2", len(tokens))
	}
	for _, rawURL := range []string{"https://github.com/org/repo/mcp", "https://api.githubcopilot.com/mcp/"} {
		if tokens[rawURL] != "gh-token" {
			t.Errorf("client token for %q = %q, want %q", rawURL, tokens[rawURL], "gh-token")
		}
	}
}

func TestMCPFactory_EnvFallback(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "env-token")

	var capturedToken string
	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			return "", errors.New("gh unavailable")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			capturedToken = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://api.githubcopilot.com/mcp/"},
	})

	if _, ok := deps["github"]; !ok {
		t.Fatal("expected 'github' dependency")
	}
	if capturedToken != "env-token" {
		t.Errorf("client token = %q, want %q", capturedToken, "env-token")
	}
}

func TestMCPFactory_TokenResolutionFailureSkips(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")

	var buf bytes.Buffer
	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		tokenResolver: func() (string, error) {
			return "", errors.New("gh unavailable")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://api.githubcopilot.com/mcp/"},
	})

	if len(deps) != 0 {
		t.Fatalf("deps count = %d, want 0 (server must be skipped)", len(deps))
	}
	if !bytes.Contains(buf.Bytes(), []byte("mcp_token_resolution_skipped")) {
		t.Errorf("expected mcp_token_resolution_skipped warning, got: %s", buf.String())
	}
}

func TestMCPFactory_ClientInitFailureSkips(t *testing.T) {
	var buf bytes.Buffer
	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			return nil, errors.New("boom")
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"local": {URL: "https://example.com/mcp"},
	})

	if len(deps) != 0 {
		t.Fatalf("deps count = %d, want 0 (server must be skipped)", len(deps))
	}
	if !bytes.Contains(buf.Bytes(), []byte("mcp_client_init_failed")) {
		t.Errorf("expected mcp_client_init_failed warning, got: %s", buf.String())
	}
}

func TestMCPFactory_ConsentAndSerialPropagation(t *testing.T) {
	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"readonly": {URL: "https://example.com/mcp/readonly"},
		"mutating": {URL: "https://example.com/mcp/"},
	})

	ro, ok := deps["readonly"]
	if !ok {
		t.Fatal("expected 'readonly' dependency")
	}
	if ro.Client == nil {
		t.Error("readonly client should be non-nil")
	}
	if ro.RequiresConsent {
		t.Error("readonly endpoint should not require consent")
	}
	if ro.Serial {
		t.Error("readonly endpoint should not be serial")
	}

	mu, ok := deps["mutating"]
	if !ok {
		t.Fatal("expected 'mutating' dependency")
	}
	if mu.Client == nil {
		t.Error("mutating client should be non-nil")
	}
	if !mu.RequiresConsent {
		t.Error("mutating endpoint should require consent")
	}
	if !mu.Serial {
		t.Error("mutating endpoint should be serial")
	}
}

func TestMCPFactory_AuthNone(t *testing.T) {
	resolverCalls := 0
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			captured[cfg.URL] = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://github.com/org/repo/mcp", Auth: config.MCPAuthNone},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (auth none must not resolve)", resolverCalls)
	}
	dep, ok := deps["github"]
	if !ok {
		t.Fatal("expected 'github' dependency")
	}
	if dep.Client == nil {
		t.Error("expected non-nil MCP client")
	}
	if got := captured["https://github.com/org/repo/mcp"]; got != "" {
		t.Errorf("client token = %q, want empty (auth none)", got)
	}
}

func TestMCPFactory_AuthBearer(t *testing.T) {
	resolverCalls := 0
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			captured[cfg.URL] = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://api.githubcopilot.com/mcp/", Auth: config.MCPAuthBearer, Token: "explicit-token"},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (bearer uses explicit token)", resolverCalls)
	}
	dep, ok := deps["github"]
	if !ok {
		t.Fatal("expected 'github' dependency")
	}
	if dep.Client == nil {
		t.Error("expected non-nil MCP client")
	}
	if got := captured["https://api.githubcopilot.com/mcp/"]; got != "explicit-token" {
		t.Errorf("client token = %q, want %q", got, "explicit-token")
	}

	// A stray USERNAME under bearer must be normalized away: only the basic
	// auth mode may carry a username (single-discriminator invariant).
	t.Run("stray USERNAME normalized", func(t *testing.T) {
		resolverCalls := 0
		var capturedUsername, capturedToken string

		f := &mcpFactory{
			logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
			tokenResolver: func() (string, error) {
				resolverCalls++
				return "", errors.New("should not be called")
			},
			newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
				capturedUsername = cfg.Username
				capturedToken = cfg.Token
				return &fakeMCPClient{}, nil
			},
		}

		deps := f.Build(map[string]config.MCPServerConfig{
			"github": {URL: "https://api.githubcopilot.com/mcp/", Auth: config.MCPAuthBearer, Username: "stray", Token: "explicit-token"},
		})

		if resolverCalls != 0 {
			t.Errorf("token resolver called %d times, want 0", resolverCalls)
		}
		if _, ok := deps["github"]; !ok {
			t.Fatal("expected 'github' dependency")
		}
		if capturedUsername != "" {
			t.Errorf("client username = %q, want empty (bearer must normalize stray username)", capturedUsername)
		}
		if capturedToken != "explicit-token" {
			t.Errorf("client token = %q, want %q", capturedToken, "explicit-token")
		}
	})
}

func TestMCPFactory_AuthBasic(t *testing.T) {
	resolverCalls := 0
	var capturedUsername, capturedToken string

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			capturedUsername = cfg.Username
			capturedToken = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"atlassian": {URL: "https://mcp.atlassian.com/v1/mcp", Auth: config.MCPAuthBasic, Username: "user@example.com", Token: "tok"},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (basic must not resolve)", resolverCalls)
	}
	dep, ok := deps["atlassian"]
	if !ok {
		t.Fatal("expected 'atlassian' dependency")
	}
	if dep.Client == nil {
		t.Error("expected non-nil MCP client")
	}
	if capturedUsername != "user@example.com" {
		t.Errorf("client username = %q, want %q", capturedUsername, "user@example.com")
	}
	if capturedToken != "tok" {
		t.Errorf("client token = %q, want %q", capturedToken, "tok")
	}
}

func TestMCPFactory_AuthGH(t *testing.T) {
	resolverCalls := 0
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			captured[cfg.URL] = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	// A private/non-GitHub hostname under "gh" still triggers gh resolution.
	deps := f.Build(map[string]config.MCPServerConfig{
		"enterprise": {URL: "https://mcp.enterprise.internal/mcp", Auth: config.MCPAuthGH},
	})

	if resolverCalls != 1 {
		t.Errorf("token resolver called %d times, want 1 (gh must resolve for any endpoint)", resolverCalls)
	}
	dep, ok := deps["enterprise"]
	if !ok {
		t.Fatal("expected 'enterprise' dependency")
	}
	if dep.Client == nil {
		t.Error("expected non-nil MCP client")
	}
	if got := captured["https://mcp.enterprise.internal/mcp"]; got != "gh-token" {
		t.Errorf("client token = %q, want %q", got, "gh-token")
	}
}

func TestMCPFactory_AuthGH_ExplicitTokenWins(t *testing.T) {
	resolverCalls := 0
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			captured[cfg.URL] = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://api.githubcopilot.com/mcp/", Auth: config.MCPAuthGH, Token: "explicit-token"},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (explicit token wins under gh)", resolverCalls)
	}
	if _, ok := deps["github"]; !ok {
		t.Fatal("expected 'github' dependency")
	}
	if got := captured["https://api.githubcopilot.com/mcp/"]; got != "explicit-token" {
		t.Errorf("client token = %q, want %q", got, "explicit-token")
	}
}

func TestMCPFactory_AuthAuto(t *testing.T) {
	resolverCalls := 0
	var mu sync.Mutex
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			mu.Lock()
			captured[cfg.URL] = cfg.Token
			mu.Unlock()
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://github.com/org/repo/mcp"},
		"local":  {URL: "https://example.com/mcp"},
	})

	if resolverCalls != 1 {
		t.Errorf("token resolver called %d times, want 1 (only GitHub hostname resolves under auto)", resolverCalls)
	}
	if len(deps) != 2 {
		t.Fatalf("deps count = %d, want 2", len(deps))
	}
	if got := captured["https://github.com/org/repo/mcp"]; got != "gh-token" {
		t.Errorf("github client token = %q, want %q", got, "gh-token")
	}
	if got := captured["https://example.com/mcp"]; got != "" {
		t.Errorf("local client token = %q, want empty (auto → none for non-GitHub)", got)
	}
}

func TestMCPFactory_AuthAuto_ExplicitTokenWins(t *testing.T) {
	resolverCalls := 0
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			captured[cfg.URL] = cfg.Token
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"github": {URL: "https://github.com/org/repo/mcp", Token: "explicit-token"},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (explicit token wins under auto)", resolverCalls)
	}
	if _, ok := deps["github"]; !ok {
		t.Fatal("expected 'github' dependency")
	}
	if got := captured["https://github.com/org/repo/mcp"]; got != "explicit-token" {
		t.Errorf("client token = %q, want %q", got, "explicit-token")
	}
}

// TestMCPFactory_StdioDispatch pins the transport dispatch: a stdio (COMMAND)
// server and a URL server are both built, and each dep carries the correct
// consent/serial policy for its transport class.
func TestMCPFactory_StdioDispatch(t *testing.T) {
	var mu sync.Mutex
	stdioSeen := 0
	urlSeen := 0

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			mu.Lock()
			defer mu.Unlock()
			if cfg.IsStdio() {
				stdioSeen++
			} else {
				urlSeen++
			}
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"fs":     {Command: "npx", Args: []string{"-y", "server"}},
		"remote": {URL: "https://example.com/mcp"},
	})

	fs, ok := deps["fs"]
	if !ok {
		t.Fatal("expected 'fs' dependency")
	}
	if fs.RequiresConsent {
		t.Error("stdio dep RequiresConsent = true, want false (trusted local spawn)")
	}
	if !fs.Serial {
		t.Error("stdio dep Serial = false, want true (no URL → serial by construction)")
	}

	remote, ok := deps["remote"]
	if !ok {
		t.Fatal("expected 'remote' dependency")
	}
	if !remote.RequiresConsent {
		t.Error("remote dep RequiresConsent = false, want true (mutating URL class)")
	}
	if !remote.Serial {
		t.Error("remote dep Serial = false, want true (mutating URL class)")
	}

	if stdioSeen != 1 || urlSeen != 1 {
		t.Errorf("fake saw stdio=%d url=%d, want exactly one of each", stdioSeen, urlSeen)
	}
}

// TestMCPFactory_StdioSkipsTokenResolution pins that a stdio server under gh
// auth short-circuits before token resolution: the resolver is never invoked
// and no mcp_token_resolution_skipped warning is emitted.
func TestMCPFactory_StdioSkipsTokenResolution(t *testing.T) {
	var buf bytes.Buffer
	resolverCalls := 0

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"fs": {Command: "npx", Auth: config.MCPAuthGH},
	})

	if resolverCalls != 0 {
		t.Errorf("token resolver called %d times, want 0 (stdio short-circuits before resolution)", resolverCalls)
	}
	if _, ok := deps["fs"]; !ok {
		t.Fatal("expected 'fs' dependency")
	}
	if bytes.Contains(buf.Bytes(), []byte("mcp_token_resolution_skipped")) {
		t.Error("stdio server must not emit mcp_token_resolution_skipped")
	}
}

// TestMCPFactory_ConstructorError_NoArgsEnvLeak pins that constructor-failure
// warning text carries the COMMAND at most — never ARGS elements or ENV
// values — and that the failing servers are skipped.
func TestMCPFactory_ConstructorError_NoArgsEnvLeak(t *testing.T) {
	var buf bytes.Buffer

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			return nil, fmt.Errorf("init failed for %s", cfg.Command)
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"fs":     {Command: "npx", Args: []string{"sk-args-leak"}, Env: map[string]string{"K": "sk-env-leak"}},
		"remote": {URL: "https://example.com/mcp"},
	})

	if len(deps) != 0 {
		t.Fatalf("deps count = %d, want 0 (both servers must be skipped)", len(deps))
	}
	out := buf.String()
	if !strings.Contains(out, "npx") {
		t.Error("log missing the COMMAND (npx); error text must carry the command name")
	}
	if strings.Contains(out, "sk-args-leak") {
		t.Error("log leaked an ARGS value (sk-args-leak)")
	}
	if strings.Contains(out, "sk-env-leak") {
		t.Error("log leaked an ENV value (sk-env-leak)")
	}
	if !bytes.Contains(buf.Bytes(), []byte("mcp_client_init_failed")) {
		t.Error("expected mcp_client_init_failed warning")
	}
}

// TestMCPFactory_ConcurrentBuild proves phase-2 construction is concurrent:
// each fake call blocks on its own release channel, so all N constructions
// must have entered the fake before any completes. A deadline select bounds
// the wait — no time.Sleep (ADR-036).
func TestMCPFactory_ConcurrentBuild(t *testing.T) {
	const n = 4

	releases := make([]chan struct{}, n)
	for i := range releases {
		releases[i] = make(chan struct{})
	}
	entered := make(chan struct{}, n)
	var started atomic.Int32

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			idx := int(started.Add(1)) - 1
			entered <- struct{}{}
			<-releases[idx]
			return &fakeMCPClient{}, nil
		},
	}

	servers := make(map[string]config.MCPServerConfig, n)
	for i := 0; i < n; i++ {
		servers[fmt.Sprintf("server-%d", i)] = config.MCPServerConfig{URL: fmt.Sprintf("https://example.com/mcp/%d", i)}
	}

	done := make(chan map[string]plugin.MCPServerDependency)
	go func() {
		done <- f.Build(servers)
	}()

	for i := 0; i < n; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d constructions started; Build is not concurrent", i, n)
		}
	}
	if got := started.Load(); got != n {
		t.Fatalf("started counter = %d, want %d", got, n)
	}

	for _, ch := range releases {
		close(ch)
	}
	deps := <-done

	if len(deps) != n {
		t.Fatalf("deps count = %d, want %d", len(deps), n)
	}
	for name := range servers {
		if _, ok := deps[name]; !ok {
			t.Errorf("missing dependency %q", name)
		}
	}
}

// TestMCPFactory_ConcurrentBuild_FailingServerSkips pins the per-server
// failure path under concurrent construction: the failing server's dep is
// absent, the others are present, and mcp_client_init_failed is logged
// exactly once.
func TestMCPFactory_ConcurrentBuild_FailingServerSkips(t *testing.T) {
	const n = 4

	releases := make([]chan struct{}, n)
	for i := range releases {
		releases[i] = make(chan struct{})
	}
	entered := make(chan struct{}, n)
	var started atomic.Int32
	var buf bytes.Buffer

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&buf, nil)),
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			idx := int(started.Add(1)) - 1
			entered <- struct{}{}
			<-releases[idx]
			if strings.HasSuffix(cfg.URL, "/fail") {
				return nil, errors.New("boom")
			}
			return &fakeMCPClient{}, nil
		},
	}

	servers := make(map[string]config.MCPServerConfig, n)
	for i := 0; i < n; i++ {
		url := fmt.Sprintf("https://example.com/mcp/%d", i)
		if i == 2 {
			url = "https://example.com/mcp/fail"
		}
		servers[fmt.Sprintf("server-%d", i)] = config.MCPServerConfig{URL: url}
	}

	done := make(chan map[string]plugin.MCPServerDependency)
	go func() {
		done <- f.Build(servers)
	}()

	for i := 0; i < n; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d of %d constructions started", i, n)
		}
	}
	for _, ch := range releases {
		close(ch)
	}
	deps := <-done

	if len(deps) != n-1 {
		t.Fatalf("deps count = %d, want %d (failing server skipped)", len(deps), n-1)
	}
	if _, ok := deps["server-2"]; ok {
		t.Error("failing server 'server-2' must be absent from deps")
	}
	for i := 0; i < n; i++ {
		if i == 2 {
			continue
		}
		name := fmt.Sprintf("server-%d", i)
		if _, ok := deps[name]; !ok {
			t.Errorf("missing dependency %q", name)
		}
	}
	if got := bytes.Count(buf.Bytes(), []byte("mcp_client_init_failed")); got != 1 {
		t.Errorf("mcp_client_init_failed logged %d times, want exactly 1", got)
	}
}

// TestMCPFactory_StdioAndHTTPMixed pins the mixed-transport pre-pass: one
// stdio server (gh mode, resolved untouched) plus two gh HTTP servers
// (single-flight token resolution). The stdio config reaches the constructor
// with its token still empty even though gh mode is set.
func TestMCPFactory_StdioAndHTTPMixed(t *testing.T) {
	resolverCalls := 0
	var mu sync.Mutex
	stdioToken := "unset"
	stdioSeen := false

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClientFor: func(cfg config.MCPServerConfig) (tools.MCPClient, error) {
			mu.Lock()
			defer mu.Unlock()
			if cfg.IsStdio() {
				stdioSeen = true
				stdioToken = cfg.Token
			}
			return &fakeMCPClient{}, nil
		},
	}

	deps := f.Build(map[string]config.MCPServerConfig{
		"fs":       {Command: "npx", Auth: config.MCPAuthGH},
		"github-1": {URL: "https://github.com/org/repo/mcp", Auth: config.MCPAuthGH},
		"github-2": {URL: "https://github.com/org/other/mcp", Auth: config.MCPAuthGH},
	})

	if resolverCalls != 1 {
		t.Errorf("token resolver called %d times, want exactly 1 (single-flight pre-pass)", resolverCalls)
	}
	if len(deps) != 3 {
		t.Fatalf("deps count = %d, want 3", len(deps))
	}
	if !stdioSeen {
		t.Error("fake never saw a stdio config")
	}
	if stdioToken != "" {
		t.Errorf("stdio cfg token = %q, want empty (stdio is resolved untouched; gh mode is inert under COMMAND)", stdioToken)
	}
}

// TestIsGitHubHostname covers the url.Parse error / empty-hostname branch
// at mcp_factory.go:195-197 plus the github-host match behavior.
func TestIsGitHubHostname(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"invalid URL", "://bad", false},
		{"empty string", "", false},
		{"no hostname", "https://", false},
		{"github host", "https://github.com", true},
		{"github enterprise host", "https://api.github.example.com", true},
		{"non-github host", "https://example.com", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGitHubHostname(tt.raw); got != tt.want {
				t.Errorf("isGitHubHostname(%q) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}
