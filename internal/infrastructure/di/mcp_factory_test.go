// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package di

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			capturedToken = token
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
	var tokens []string

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			tokens = append(tokens, token)
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
		t.Fatalf("client constructor called %d times, want 2", len(tokens))
	}
	for i, tok := range tokens {
		if tok != "gh-token" {
			t.Errorf("token[%d] = %q, want %q", i, tok, "gh-token")
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			capturedToken = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
			newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
				capturedUsername = username
				capturedToken = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			capturedUsername = username
			capturedToken = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
	captured := map[string]string{}

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "gh-token", nil
		},
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
		newClient: func(endpoint, username, token string, timeout time.Duration) (tools.MCPClient, error) {
			captured[endpoint] = token
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
