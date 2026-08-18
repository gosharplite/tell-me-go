// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestCallTool_WireArguments_NilBecomesEmptyObject pins the #1399 wire
// regression over the HTTP transport: a nil arguments value must reach the
// server as "{}", not "null". A typed-nil map[string]interface{} stored in
// the SDK's CallToolParams.Arguments `any` field defeats both omitempty and
// the SDK's interface-nil guard, so pre-fix the wire carries
// `"arguments": null` and the server echoes "null". RED pre-fix: fails with
// Text = "null", want "{}".
func TestCallTool_WireArguments_NilBecomesEmptyObject(t *testing.T) {
	toolsList := []*sdkmcp.Tool{
		{Name: "args-capture", Description: "returns the raw arguments bytes as text", InputSchema: objectSchema(nil)},
	}
	handlers := map[string]sdkmcp.ToolHandler{
		"args-capture": func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
		},
	}

	ts := newSDKTestServer(t, toolsList, handlers)
	defer ts.Close()

	c, err := NewClient(ts.URL, "", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	res, err := c.CallTool(context.Background(), "args-capture", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.Error != nil {
		t.Errorf("Error = %v, want nil", res.Error)
	}
	if res.Text != "{}" {
		t.Errorf("Text = %q, want %q", res.Text, "{}")
	}
}

// TestCallTool_WireArguments_NonEmptyPassthrough pins that non-nil arguments
// pass through verbatim over HTTP (green both pre- and post-fix).
func TestCallTool_WireArguments_NonEmptyPassthrough(t *testing.T) {
	toolsList := []*sdkmcp.Tool{
		{Name: "args-capture", Description: "returns the raw arguments bytes as text", InputSchema: objectSchema(nil)},
	}
	handlers := map[string]sdkmcp.ToolHandler{
		"args-capture": func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(req.Params.Arguments)}}}, nil
		},
	}

	ts := newSDKTestServer(t, toolsList, handlers)
	defer ts.Close()

	c, err := NewClient(ts.URL, "", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	res, err := c.CallTool(context.Background(), "args-capture", map[string]interface{}{"text": "x"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.Error != nil {
		t.Errorf("Error = %v, want nil", res.Error)
	}
	if !strings.Contains(res.Text, "text") {
		t.Errorf("Text = %q, want it to contain %q", res.Text, "text")
	}
}

// TestStdio_WireArguments_NilBecomesEmptyObject pins the same #1399 wire
// regression over the stdio transport against the real fixture binary's
// args-capture tool. RED pre-fix: fails with Text = "null", want "{}".
func TestStdio_WireArguments_NilBecomesEmptyObject(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 0, slog.Default())

	res, err := c.CallTool(context.Background(), "args-capture", nil)
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.Error != nil {
		t.Errorf("Error = %v, want nil", res.Error)
	}
	if res.Text != "{}" {
		t.Errorf("Text = %q, want %q", res.Text, "{}")
	}
}

// TestStdio_WireArguments_NonEmptyPassthrough pins that non-nil arguments
// pass through verbatim over stdio (green both pre- and post-fix).
func TestStdio_WireArguments_NonEmptyPassthrough(t *testing.T) {
	c := newTestClient(t, []string{"serve"}, 0, slog.Default())

	res, err := c.CallTool(context.Background(), "args-capture", map[string]interface{}{"text": "x"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if res.Error != nil {
		t.Errorf("Error = %v, want nil", res.Error)
	}
	if !strings.Contains(res.Text, "text") {
		t.Errorf("Text = %q, want it to contain %q", res.Text, "text")
	}
}

// TestNormalizeArguments pins the normalizeArguments contract (issue #1399):
// a nil map becomes a non-nil empty map — the SDK must never receive a
// typed-nil map in the CallToolParams.Arguments any-field, which would emit
// "arguments": null on the wire — while a non-nil map is returned unchanged.
func TestNormalizeArguments(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantLen int
	}{
		{"nil becomes empty", nil, 0},
		{"non-nil empty unchanged", map[string]interface{}{}, 0},
		{"non-empty unchanged", map[string]interface{}{"text": "x"}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeArguments(tt.args)
			if got == nil {
				t.Fatalf("normalizeArguments(%v) = nil, want non-nil", tt.args)
			}
			if len(got) != tt.wantLen {
				t.Errorf("len(normalizeArguments(%v)) = %d, want %d", tt.args, len(got), tt.wantLen)
			}
			if tt.args != nil && !reflect.DeepEqual(got, tt.args) {
				t.Errorf("normalizeArguments(%v) = %v, want the input unchanged", tt.args, got)
			}
		})
	}
}
