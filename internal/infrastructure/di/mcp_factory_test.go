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

func TestMCPFactory_ExplicitToken(t *testing.T) {
	resolverCalls := 0
	var capturedToken string

	f := &mcpFactory{
		logger: slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		tokenResolver: func() (string, error) {
			resolverCalls++
			return "", errors.New("should not be called")
		},
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
		newClient: func(endpoint, token string, timeout time.Duration) (tools.MCPClient, error) {
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
