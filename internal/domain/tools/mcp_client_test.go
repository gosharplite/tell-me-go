// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// testMCPClient is a hand-rolled function-field test double for MCPClient
// (ADR-021: test doubles use hand-rolled function-field mocks, not testify/mock).
type testMCPClient struct {
	listToolsFunc func(ctx context.Context) ([]MCPToolDefinition, error)
	callToolFunc  func(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error)
	closeFunc     func() error
}

func (m *testMCPClient) ListTools(ctx context.Context) ([]MCPToolDefinition, error) {
	if m.listToolsFunc != nil {
		return m.listToolsFunc(ctx)
	}
	return nil, nil
}

func (m *testMCPClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, name, args)
	}
	return ToolResult{}, nil
}

func (m *testMCPClient) Close() error {
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return nil
}

// Compile-time interface satisfaction check.
var _ MCPClient = (*testMCPClient)(nil)

func TestMCPToolDefinition_Marshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   MCPToolDefinition
		want string
	}{
		{
			name: "all fields populated",
			in: MCPToolDefinition{
				Name:        "search_repos",
				Description: "Search GitHub repositories",
				InputSchema: json.RawMessage(`{"type":"OBJECT","properties":{"q":{"type":"STRING"}}}`),
			},
			want: `{"name":"search_repos","description":"Search GitHub repositories","inputSchema":{"type":"OBJECT","properties":{"q":{"type":"STRING"}}}}`,
		},
		{
			name: "omitempty drops description and inputSchema",
			in: MCPToolDefinition{
				Name: "ping",
			},
			want: `{"name":"ping"}`,
		},
		{
			name: "omitempty drops empty inputSchema only",
			in: MCPToolDefinition{
				Name:        "ping",
				Description: "Ping the server",
			},
			want: `{"name":"ping","description":"Ping the server"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := json.Marshal(tt.in)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("json.Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestMCPToolDefinition_Unmarshal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		in      string
		want    MCPToolDefinition
		wantErr bool
	}{
		{
			name: "all fields",
			in:   `{"name":"search_repos","description":"Search GitHub repositories","inputSchema":{"type":"OBJECT"}}`,
			want: MCPToolDefinition{
				Name:        "search_repos",
				Description: "Search GitHub repositories",
				InputSchema: json.RawMessage(`{"type":"OBJECT"}`),
			},
		},
		{
			name: "missing optional fields",
			in:   `{"name":"ping"}`,
			want: MCPToolDefinition{
				Name: "ping",
			},
		},
		{
			name:    "invalid JSON",
			in:      `{`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var got MCPToolDefinition
			err := json.Unmarshal([]byte(tt.in), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("json.Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if string(got.InputSchema) != string(tt.want.InputSchema) {
				t.Errorf("InputSchema = %s, want %s", got.InputSchema, tt.want.InputSchema)
			}
		})
	}
}

func TestMCPToolDefinition_InputSchemaRoundTrip(t *testing.T) {
	t.Parallel()

	raw := json.RawMessage(`{"type":"OBJECT","properties":{"owner":{"type":"STRING"},"count":{"type":"INTEGER"}},"required":["owner"]}`)
	def := MCPToolDefinition{
		Name:        "list_issues",
		Description: "List issues for a repository",
		InputSchema: raw,
	}

	b, err := json.Marshal(def)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got MCPToolDefinition
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	if string(got.InputSchema) != string(raw) {
		t.Errorf("InputSchema round-trip = %s, want %s", got.InputSchema, raw)
	}
}

func TestTestMCPClient_SatisfiesInterface(t *testing.T) {
	t.Parallel()

	// The compile-time assertion above is the primary guarantee; this test
	// exercises the double at runtime to confirm the method dispatch works.
	ctx := context.Background()
	wantDefs := []MCPToolDefinition{{Name: "tool_a"}}
	wantResult := ToolResult{Text: "done"}

	var gotName string
	mc := &testMCPClient{
		listToolsFunc: func(ctx context.Context) ([]MCPToolDefinition, error) {
			return wantDefs, nil
		},
		callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error) {
			gotName = name
			return wantResult, nil
		},
	}

	defs, err := mc.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "tool_a" {
		t.Errorf("ListTools() = %+v, want one tool_a", defs)
	}

	res, err := mc.CallTool(ctx, "tool_a", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if gotName != "tool_a" {
		t.Errorf("CallTool name = %q, want %q", gotName, "tool_a")
	}
	if res.Text != "done" {
		t.Errorf("CallTool().Text = %q, want %q", res.Text, "done")
	}

	if err := mc.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestTestMCPClient_ErrorPropagation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	listErr := errors.New("list failed")
	callErr := errors.New("call failed")
	closeErr := errors.New("close failed")

	mc := &testMCPClient{
		listToolsFunc: func(ctx context.Context) ([]MCPToolDefinition, error) {
			return nil, listErr
		},
		callToolFunc: func(ctx context.Context, name string, args map[string]interface{}) (ToolResult, error) {
			return ToolResult{}, callErr
		},
		closeFunc: func() error {
			return closeErr
		},
	}

	if _, err := mc.ListTools(ctx); !errors.Is(err, listErr) {
		t.Errorf("ListTools() error = %v, want %v", err, listErr)
	}
	if _, err := mc.CallTool(ctx, "x", nil); !errors.Is(err, callErr) {
		t.Errorf("CallTool() error = %v, want %v", err, callErr)
	}
	if err := mc.Close(); !errors.Is(err, closeErr) {
		t.Errorf("Close() error = %v, want %v", err, closeErr)
	}
}
