// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

// registerProxyTool registers a single MCP tool named "search" whose CallTool
// behavior is controlled by callFunc, and returns the proxied handler together
// with the registry used to register it.
func registerProxyTool(t *testing.T, callFunc func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)) (tools.ToolFunc, *mockRegistry) {
	t.Helper()

	reg := &mockRegistry{}
	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{{Name: "search"}}, nil
		},
		callToolFunc: callFunc,
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"srv": {Client: client},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(reg.registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(reg.registered))
	}

	return reg.registered[0].handler, reg
}

// TestProxyHandler_ErrorSplit verifies the three-way error translation of the
// proxy handler: clean success, a non-terminal MCP tool error carried in
// ToolResult.Error, and a terminal transport error surfaced as a Go error.
func TestProxyHandler_ErrorSplit(t *testing.T) {
	t.Parallel()

	t.Run("clean success", func(t *testing.T) {
		t.Parallel()

		want := tools.ToolResult{
			Text: "result",
			Metadata: map[string]interface{}{
				"structuredContent": map[string]interface{}{
					"items": []interface{}{"a", "b"},
				},
			},
		}
		handler, _ := registerProxyTool(t, func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return want, nil
		})

		got, err := handler(context.Background(), map[string]interface{}{"q": "x"}, nil)
		if err != nil {
			t.Fatalf("handler() Go error = %v, want nil", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("handler() result = %#v, want %#v", got, want)
		}
	})

	t.Run("mcp tool error", func(t *testing.T) {
		t.Parallel()

		toolErr := errors.New("bad query")
		handler, _ := registerProxyTool(t, func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{Text: "bad query", Error: toolErr}, nil
		})

		got, err := handler(context.Background(), map[string]interface{}{"q": "x"}, nil)
		if err != nil {
			t.Fatalf("handler() Go error = %v, want nil", err)
		}
		if got.Error == nil {
			t.Fatal("handler() ToolResult.Error = nil, want non-nil")
		}
		if got.Error.Error() != "bad query" {
			t.Errorf("handler() ToolResult.Error = %q, want %q", got.Error.Error(), "bad query")
		}
		if got.Text != "bad query" {
			t.Errorf("handler() Text = %q, want %q", got.Text, "bad query")
		}
	})

	t.Run("terminal transport error", func(t *testing.T) {
		t.Parallel()

		transportErr := errors.New("connection reset")
		handler, _ := registerProxyTool(t, func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			return tools.ToolResult{}, transportErr
		})

		_, err := handler(context.Background(), map[string]interface{}{"q": "x"}, nil)
		if err == nil {
			t.Fatal("handler() Go error = nil, want non-nil")
		}
		if !errors.Is(err, transportErr) {
			t.Errorf("handler() Go error = %v, want %v", err, transportErr)
		}
	})
}

// TestProxyHandler_ArgumentsAndContextForwarding verifies that the proxy
// handler passes the caller's arguments map unchanged and forwards the caller's
// context (including cancellation) to CallTool.
func TestProxyHandler_ArgumentsAndContextForwarding(t *testing.T) {
	t.Parallel()

	t.Run("arguments forwarded unchanged", func(t *testing.T) {
		t.Parallel()

		wantArgs := map[string]interface{}{"q": "tell-me-go", "limit": 10}
		var gotArgs map[string]interface{}

		handler, _ := registerProxyTool(t, func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotArgs = args
			return tools.ToolResult{Text: "ok"}, nil
		})

		if _, err := handler(context.Background(), wantArgs, nil); err != nil {
			t.Fatalf("handler() error = %v", err)
		}
		if !reflect.DeepEqual(gotArgs, wantArgs) {
			t.Errorf("CallTool args = %#v, want %#v", gotArgs, wantArgs)
		}
	})

	t.Run("context cancellation forwarded", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		var gotCtxErr error
		handler, _ := registerProxyTool(t, func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			gotCtxErr = ctx.Err()
			return tools.ToolResult{Text: "ok"}, nil
		})

		if _, err := handler(ctx, nil, nil); err != nil {
			t.Fatalf("handler() error = %v", err)
		}
		if !errors.Is(gotCtxErr, context.Canceled) {
			t.Errorf("CallTool ctx.Err() = %v, want context.Canceled", gotCtxErr)
		}
	})
}

// TestProxyRegister_MultiServerNameDisambiguation verifies that two servers
// exposing the same tool name register into one registry under distinct,
// deterministic namespaced names without collision.
func TestProxyRegister_MultiServerNameDisambiguation(t *testing.T) {
	t.Parallel()

	reg := &mockRegistry{}
	mk := func() *mockMCPClient {
		return &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
				return []tools.MCPToolDefinition{{Name: "search"}}, nil
			},
			callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
				return tools.ToolResult{Text: "ok:" + name}, nil
			},
		}
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"github": {Client: mk()},
			"gitlab": {Client: mk()},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	if len(reg.registered) != 2 {
		t.Fatalf("registered %d tools, want 2", len(reg.registered))
	}

	got := []string{reg.registered[0].decl.Name, reg.registered[1].decl.Name}
	want := []string{"mcp_github_search", "mcp_gitlab_search"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("registered names = %v, want %v", got, want)
	}
	if got[0] == got[1] {
		t.Errorf("registered names collided: %q", got[0])
	}
}

// TestProxyRegister_TruncatedToolName verifies that a server tool name longer
// than 64 bytes is namespaced to a bounded, hashed name, while the proxy still
// forwards the original full tool name to CallTool.
func TestProxyRegister_TruncatedToolName(t *testing.T) {
	t.Parallel()

	longTool := repeat("a", 100)
	reg := &mockRegistry{}
	var calledName string

	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{{Name: longTool}}, nil
		},
		callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			calledName = name
			return tools.ToolResult{Text: "ok"}, nil
		},
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"github": {Client: client},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(reg.registered) != 1 {
		t.Fatalf("registered %d tools, want 1", len(reg.registered))
	}

	decl := reg.registered[0].decl
	if len(decl.Name) > 64 {
		t.Errorf("namespaced name length = %d, exceeds 64: %q", len(decl.Name), decl.Name)
	}
	if decl.Name == "mcp_github_"+longTool {
		t.Error("namespaced name was not truncated")
	}

	// Executing the shortened tool name must still forward the original full
	// tool name to CallTool.
	if _, err := reg.registered[0].handler(context.Background(), nil, nil); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if calledName != longTool {
		t.Errorf("CallTool name = %q, want original full tool name %q", calledName, longTool)
	}
}

// TestProxyRegister_ExecutionOptions verifies the registered ToolOptions and
// ToolDeclaration mirror the server dependency's execution policy.
func TestProxyRegister_ExecutionOptions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		serial  bool
		consent bool
	}{
		{name: "serial and consent", serial: true, consent: true},
		{name: "neither serial nor consent", serial: false, consent: false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reg := &mockRegistry{}
			client := &mockMCPClient{
				listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
					return []tools.MCPToolDefinition{{Name: "search"}}, nil
				},
			}

			deps := plugin.PluginDependencies{
				MCPClients: map[string]plugin.MCPServerDependency{
					"srv": {Client: client, RequiresConsent: tc.consent, Serial: tc.serial},
				},
			}

			p := NewPlugin()
			if err := p.Register(reg, deps); err != nil {
				t.Fatalf("Register() error = %v", err)
			}
			if len(reg.registered) != 1 {
				t.Fatalf("registered %d tools, want 1", len(reg.registered))
			}

			rt := reg.registered[0]
			if rt.opts.LongRunning != true {
				t.Errorf("LongRunning = %v, want true", rt.opts.LongRunning)
			}
			if rt.opts.LivenessThreshold != 0 {
				t.Errorf("LivenessThreshold = %v, want 0", rt.opts.LivenessThreshold)
			}
			if rt.opts.Serial != tc.serial {
				t.Errorf("Serial = %v, want %v", rt.opts.Serial, tc.serial)
			}
			if rt.decl.RequiresConsent != tc.consent {
				t.Errorf("RequiresConsent = %v, want %v", rt.decl.RequiresConsent, tc.consent)
			}
		})
	}
}
