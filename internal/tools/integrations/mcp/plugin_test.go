// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/integrations/plugin"
)

func TestFormatToolName_Short(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		server string
		tool   string
		want   string
	}{
		{name: "basic", server: "github", tool: "search_repos", want: "mcp_github_search_repos"},
		{name: "empty tool", server: "srv", tool: "", want: "mcp_srv_"},
		{name: "empty server", server: "", tool: "tool", want: "mcp__tool"},
		{name: "exactly 64", server: "s", tool: repeat("t", 58), want: "mcp_s_" + repeat("t", 58)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := FormatToolName(tc.server, tc.tool)
			if got != tc.want {
				t.Errorf("FormatToolName() = %q, want %q", got, tc.want)
			}
			if len(got) > 64 {
				t.Errorf("FormatToolName() length = %d, exceeds 64", len(got))
			}
		})
	}
}

func TestFormatToolName_Long(t *testing.T) {
	t.Parallel()

	longTool := repeat("a", 100)
	got := FormatToolName("github", longTool)

	if len(got) > 64 {
		t.Errorf("FormatToolName() length = %d, exceeds 64: %q", len(got), got)
	}
	if len(got) < len("mcp_github__")+8 {
		t.Errorf("FormatToolName() = %q, missing hash suffix", got)
	}

	// Deterministic: same inputs produce the same name.
	if again := FormatToolName("github", longTool); again != got {
		t.Errorf("FormatToolName() not deterministic: %q vs %q", got, again)
	}

	// Distinct tools produce distinct names (hash disambiguation).
	other := repeat("b", 100)
	if FormatToolName("github", other) == got {
		t.Errorf("distinct long tools collided")
	}
}

func TestFormatToolName_PrefixCap(t *testing.T) {
	t.Parallel()

	// A very short server name leaves a budget > 40, which must be capped at 40.
	longTool := repeat("x", 200)
	got := FormatToolName("a", longTool)

	// "mcp_a_" (6) + prefix(40) + "_" (1) + hash8 (8) = 55.
	if len(got) != 55 {
		t.Errorf("FormatToolName() length = %d, want 55: %q", len(got), got)
	}
	if got[:6] != "mcp_a_" {
		t.Errorf("FormatToolName() prefix = %q, want %q", got[:6], "mcp_a_")
	}
	if got[6:46] != repeat("x", 40) {
		t.Errorf("FormatToolName() tool prefix length = %d, want 40 runes", len(got[6:46]))
	}
}

func TestFormatToolName_LongServerClamped(t *testing.T) {
	t.Parallel()

	// A very long server name can exhaust the entire budget; the helper must
	// not panic and must not slice out of range.
	longServer := repeat("s", 100)
	got := FormatToolName(longServer, "tool")
	if len(got) <= 0 {
		t.Error("FormatToolName() returned empty string")
	}
	_ = got
}

// repeat returns s concatenated n times.
func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

// --- Register behavior ---

// mockRegistry is a hand-rolled test double for tools.Registry (ADR-021).
type mockRegistry struct {
	registered  []registeredTool
	registerErr error
}

type registeredTool struct {
	decl    *tools.ToolDeclaration
	handler tools.ToolFunc
	opts    tools.ToolOptions
}

func (m *mockRegistry) Register(def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.RegisterWithOptions(def, handler, tools.ToolOptions{})
}

func (m *mockRegistry) RegisterWithOptions(def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	m.registered = append(m.registered, registeredTool{decl: def, handler: handler, opts: opts})
	return nil
}

func (m *mockRegistry) RegisterToToolkit(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc) error {
	return m.Register(def, handler)
}

func (m *mockRegistry) RegisterToToolkitWithOptions(toolkit string, def *tools.ToolDeclaration, handler tools.ToolFunc, opts tools.ToolOptions) error {
	return m.RegisterWithOptions(def, handler, opts)
}

func (m *mockRegistry) Execute(ctx context.Context, name string, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func (m *mockRegistry) IsSerial(name string) bool                     { return false }
func (m *mockRegistry) IsLongRunning(name string) bool                { return false }
func (m *mockRegistry) GetOptions(name string) tools.ToolOptions      { return tools.ToolOptions{} }
func (m *mockRegistry) GetDeclarations() []*tools.ToolDeclaration     { return nil }
func (m *mockRegistry) GetCoreDeclarations() []*tools.ToolDeclaration { return nil }
func (m *mockRegistry) GetDeclarationsByToolkits(toolkits []string) []*tools.ToolDeclaration {
	return nil
}
func (m *mockRegistry) ListAvailableToolkits() []string { return nil }

// mockMCPClient is a hand-rolled function-field test double for tools.MCPClient.
type mockMCPClient struct {
	listToolsFunc func(ctx context.Context) ([]tools.MCPToolDefinition, error)
	callToolFunc  func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error)
}

func (m *mockMCPClient) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	if m.listToolsFunc != nil {
		return m.listToolsFunc(ctx)
	}
	return nil, nil
}

func (m *mockMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, name, args)
	}
	return tools.ToolResult{}, nil
}

func (m *mockMCPClient) Close() error { return nil }

func TestRegister_NoClients(t *testing.T) {
	t.Parallel()

	p := NewPlugin()
	if err := p.Register(&mockRegistry{}, plugin.PluginDependencies{}); err != nil {
		t.Fatalf("Register() error = %v, want nil", err)
	}
}

func TestRegister_RegistersDiscoveredTools(t *testing.T) {
	t.Parallel()

	reg := &mockRegistry{}
	callCount := 0
	var calledName string
	var calledArgs map[string]interface{}

	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{
				{Name: "search", Description: "search stuff", InputSchema: rawJSON(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)},
				{Name: "create", Description: "create stuff", InputSchema: rawJSON(`{}`)},
			}, nil
		},
		callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
			callCount++
			calledName = name
			calledArgs = args
			return tools.ToolResult{Text: "ok"}, nil
		},
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"github": {Client: client, RequiresConsent: true, Serial: true},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if len(reg.registered) != 2 {
		t.Fatalf("registered %d tools, want 2", len(reg.registered))
	}

	assertFirstRegisteredTool(t, reg)
	assertSecondRegisteredTool(t, reg)

	// Invoke the proxied handler to confirm it forwards to CallTool with the
	// original (unnamespaced) tool name.
	if _, err := reg.registered[0].handler(context.Background(), map[string]interface{}{"q": "x"}, nil); err != nil {
		t.Fatalf("handler() error = %v", err)
	}
	if callCount != 1 {
		t.Errorf("CallTool call count = %d, want 1", callCount)
	}
	if calledName != "search" {
		t.Errorf("CallTool name = %q, want %q", calledName, "search")
	}
	if calledArgs["q"] != "x" {
		t.Errorf("CallTool args = %v, want q=x", calledArgs)
	}
}

// assertFirstRegisteredTool pins the first (namespaced) registered tool: the
// deterministic mcp_github_search name, consent requirement, non-nil parameter
// schema, and the Serial+LongRunning options.
func assertFirstRegisteredTool(t *testing.T, reg *mockRegistry) {
	t.Helper()
	first := reg.registered[0]
	if first.decl.Name != "mcp_github_search" {
		t.Errorf("first decl name = %q, want %q", first.decl.Name, "mcp_github_search")
	}
	if !first.decl.RequiresConsent {
		t.Error("first decl RequiresConsent = false, want true")
	}
	if first.decl.Parameters == nil {
		t.Error("first decl Parameters = nil, want non-nil schema")
	}
	if first.opts.Serial != true || first.opts.LongRunning != true {
		t.Errorf("first opts = %+v, want Serial+LongRunning", first.opts)
	}
}

// assertSecondRegisteredTool pins the second registered tool: its empty schema
// ({}) degrades to a nil Parameters (freeform args).
func assertSecondRegisteredTool(t *testing.T, reg *mockRegistry) {
	t.Helper()
	second := reg.registered[1]
	if second.decl.Parameters != nil {
		t.Errorf("second decl Parameters = %+v, want nil", second.decl.Parameters)
	}
}

func TestRegister_DiscoveryErrorContinues(t *testing.T) {
	t.Parallel()

	reg := &mockRegistry{}
	good := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{{Name: "ping"}}, nil
		},
	}
	bad := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return nil, errors.New("boom")
		},
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"bad":  {Client: bad},
			"good": {Client: good},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// The bad server's discovery error must not abort the good server.
	if len(reg.registered) != 1 {
		t.Fatalf("registered %d tools, want 1 (from good server)", len(reg.registered))
	}
	if reg.registered[0].decl.Name != "mcp_good_ping" {
		t.Errorf("decl name = %q, want %q", reg.registered[0].decl.Name, "mcp_good_ping")
	}
}

func TestRegister_DeterministicServerOrder(t *testing.T) {
	t.Parallel()

	reg := &mockRegistry{}
	mk := func() *mockMCPClient {
		return &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
				return []tools.MCPToolDefinition{{Name: "t"}}, nil
			},
		}
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"zebra": {Client: mk()},
			"alpha": {Client: mk()},
			"mike":  {Client: mk()},
		},
	}

	p := NewPlugin()
	if err := p.Register(reg, deps); err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	want := []string{"mcp_alpha_t", "mcp_mike_t", "mcp_zebra_t"}
	if len(reg.registered) != len(want) {
		t.Fatalf("registered %d tools, want %d", len(reg.registered), len(want))
	}
	for i, w := range want {
		if reg.registered[i].decl.Name != w {
			t.Errorf("registered[%d] = %q, want %q", i, reg.registered[i].decl.Name, w)
		}
	}
}

func TestRegister_RegistrationErrorBubbles(t *testing.T) {
	t.Parallel()

	reg := &mockRegistry{registerErr: errors.New("dup")}
	client := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{{Name: "t"}}, nil
		},
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"srv": {Client: client},
		},
	}

	p := NewPlugin()
	err := p.Register(reg, deps)
	if err == nil {
		t.Fatal("Register() error = nil, want error")
	}
}

func TestAutoRegistration(t *testing.T) {
	t.Parallel()

	found := false
	for _, p := range plugin.All() {
		if p.Name() == "mcp" {
			found = true
			break
		}
	}
	if !found {
		t.Error("mcp plugin not auto-registered via init()")
	}
}

// --- Concurrent & bounded discovery ---

// waitForRegisterDone waits for Register to finish, failing the test on error
// or timeout. Register is run in its own goroutine so that a discovery bug
// that blocks forever fails the test instead of hanging it.
func waitForRegisterDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Register() did not return")
	}
}

// assertRegisteredNames asserts the exact registration order recorded by the
// mock registry.
func assertRegisteredNames(t *testing.T, reg *mockRegistry, want []string) {
	t.Helper()
	if len(reg.registered) != len(want) {
		t.Fatalf("registered %d tools, want %d", len(reg.registered), len(want))
	}
	for i, w := range want {
		if reg.registered[i].decl.Name != w {
			t.Errorf("registered[%d] = %q, want %q", i, reg.registered[i].decl.Name, w)
		}
	}
}

func TestRegister_ConcurrentDiscovery(t *testing.T) {
	const numServers = 3

	// Each server signals it has entered ListTools, then blocks until the
	// shared release channel closes. A sequential implementation would block
	// on the first server and never let the others start.
	started := make(chan struct{}, numServers)
	release := make(chan struct{})

	mk := func() *mockMCPClient {
		return &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
				started <- struct{}{}
				<-release
				return []tools.MCPToolDefinition{{Name: "t"}}, nil
			},
		}
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"alpha": {Client: mk()},
			"beta":  {Client: mk()},
			"gamma": {Client: mk()},
		},
	}

	reg := &mockRegistry{}
	p := NewPlugin()
	done := make(chan error, 1)
	go func() { done <- p.Register(reg, deps) }()

	for i := 0; i < numServers; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatalf("discovery was not concurrent: %d/%d servers started", i, numServers)
		}
	}
	close(release)
	waitForRegisterDone(t, done)

	assertRegisteredNames(t, reg, []string{"mcp_alpha_t", "mcp_beta_t", "mcp_gamma_t"})
}

func TestRegister_DiscoveryTimeout(t *testing.T) {
	// Shorten the discovery window so the timeout path runs quickly.
	original := discoveryTimeout
	discoveryTimeout = 20 * time.Millisecond
	t.Cleanup(func() { discoveryTimeout = original })

	// Capture the default logger to assert the discovery-failure warning.
	var logBuf bytes.Buffer
	originalLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(originalLogger) })

	ctxCancelled := make(chan struct{})
	slow := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			<-ctx.Done()
			close(ctxCancelled)
			return nil, ctx.Err()
		},
	}
	fast := &mockMCPClient{
		listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
			return []tools.MCPToolDefinition{{Name: "ping"}}, nil
		},
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"fast": {Client: fast},
			"slow": {Client: slow},
		},
	}

	reg := &mockRegistry{}
	p := NewPlugin()
	done := make(chan error, 1)
	go func() { done <- p.Register(reg, deps) }()

	waitForRegisterDone(t, done)

	// The slow server's context must have been cancelled by the timeout.
	select {
	case <-ctxCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("slow server's context was not cancelled by the discovery timeout")
	}

	// The fast server must not be blocked by the slow server's timeout; the
	// slow server is skipped.
	assertRegisteredNames(t, reg, []string{"mcp_fast_ping"})

	if !strings.Contains(logBuf.String(), "mcp_server_discovery_failed") {
		t.Errorf("expected discovery-failure warning, got %q", logBuf.String())
	}
}

func TestRegister_DeterministicOrderingUnderConcurrency(t *testing.T) {
	names := []string{"alpha", "mike", "zebra"}
	release := make(map[string]chan struct{}, len(names))
	for _, n := range names {
		release[n] = make(chan struct{})
	}
	completed := make(chan string, len(names))

	mk := func(name string) *mockMCPClient {
		return &mockMCPClient{
			listToolsFunc: func(ctx context.Context) ([]tools.MCPToolDefinition, error) {
				<-release[name]
				completed <- name
				return []tools.MCPToolDefinition{{Name: "t"}}, nil
			},
		}
	}

	deps := plugin.PluginDependencies{
		MCPClients: map[string]plugin.MCPServerDependency{
			"alpha": {Client: mk("alpha")},
			"mike":  {Client: mk("mike")},
			"zebra": {Client: mk("zebra")},
		},
	}

	reg := &mockRegistry{}
	p := NewPlugin()
	done := make(chan error, 1)
	go func() { done <- p.Register(reg, deps) }()

	// Release servers in reverse alphabetical order, waiting for each to
	// complete before releasing the next. This deterministically forces a
	// non-alphabetical completion order; registration must still be
	// alphabetical.
	for i := len(names) - 1; i >= 0; i-- {
		close(release[names[i]])
		select {
		case <-completed:
		case <-time.After(5 * time.Second):
			t.Fatalf("server %q did not complete after release", names[i])
		}
	}
	waitForRegisterDone(t, done)

	assertRegisteredNames(t, reg, []string{"mcp_alpha_t", "mcp_mike_t", "mcp_zebra_t"})
}

// rawJSON is a test helper returning a json.RawMessage from a string literal.
func rawJSON(s string) json.RawMessage {
	return json.RawMessage(s)
}
