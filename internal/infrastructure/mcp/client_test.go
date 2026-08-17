// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"go.uber.org/goleak"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// roundTripFunc adapts a function to http.RoundTripper for deterministic
// transport-level tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// objectSchema builds a minimal MCP tool input schema rooted at "object".
func objectSchema(props map[string]any) map[string]any {
	s := map[string]any{"type": "object"}
	if props != nil {
		s["properties"] = props
	}
	return s
}

// newSDKTestServer starts an httptest server backed by a real SDK MCP server
// with the given tools registered. It runs in stateless mode so every request
// is served by an ephemeral session (no persistent server-side goroutines), and
// uses JSON responses for determinism.
func newSDKTestServer(t *testing.T, tools []*sdkmcp.Tool, handlers map[string]sdkmcp.ToolHandler) *httptest.Server {
	t.Helper()

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test-server", Version: "v1.0.0"}, nil)
	for _, tl := range tools {
		h, ok := handlers[tl.Name]
		if !ok {
			t.Fatalf("missing handler for tool %q", tl.Name)
		}
		server.AddTool(tl, h)
	}

	handler := sdkmcp.NewStreamableHTTPHandler(
		func(r *http.Request) *sdkmcp.Server { return server },
		&sdkmcp.StreamableHTTPOptions{Stateless: true, JSONResponse: true},
	)
	return httptest.NewServer(handler)
}

// sortDefs returns a name-sorted copy of defs for order-independent assertions.
func sortDefs(defs []tools.MCPToolDefinition) []tools.MCPToolDefinition {
	out := append([]tools.MCPToolDefinition(nil), defs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// decodeJSON decodes a JSON value into an any for order-insensitive comparison.
func decodeJSON(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	return v
}

func TestNewClient(t *testing.T) {
	t.Run("empty endpoint rejected", func(t *testing.T) {
		if _, err := NewClient("", "", 0); err == nil {
			t.Fatal("expected error for empty endpoint")
		}
	})

	t.Run("default timeout and option", func(t *testing.T) {
		custom := &http.Client{}
		c, err := NewClient("http://example.com/mcp", "tok", 0, WithHTTPClient(custom))
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		if c.timeout != defaultTimeout {
			t.Errorf("timeout = %v, want %v", c.timeout, defaultTimeout)
		}
		if c.httpClient != custom {
			t.Errorf("httpClient = %v, want %v", c.httpClient, custom)
		}
		if c.token != "tok" {
			t.Errorf("token = %q, want %q", c.token, "tok")
		}
	})
}

func TestWithBearerToken(t *testing.T) {
	c := &http.Client{}

	if got := withBearerToken(c, ""); got != c {
		t.Errorf("withBearerToken(empty) = %p, want original %p", got, c)
	}

	got := withBearerToken(c, "secret")
	if got == c {
		t.Fatal("withBearerToken(non-empty) should wrap the client")
	}
	if _, ok := got.Transport.(*bearerTokenTransport); !ok {
		t.Errorf("Transport = %T, want *bearerTokenTransport", got.Transport)
	}
}

func TestBearerTokenTransport(t *testing.T) {
	base := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := req.Header.Get("Authorization"); got != "Bearer secret" {
			t.Errorf("Authorization = %q, want %q", got, "Bearer secret")
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	})

	tr := &bearerTokenTransport{base: base, token: "secret"}
	req, err := http.NewRequest(http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := tr.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	_ = resp.Body.Close()

	// The original request must not be mutated.
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("original request Authorization = %q, want empty (request must be cloned)", got)
	}
}

func TestMarshalInputSchema(t *testing.T) {
	t.Run("nil schema", func(t *testing.T) {
		got, err := marshalInputSchema(nil)
		if err != nil {
			t.Fatalf("marshalInputSchema(nil) error = %v", err)
		}
		if got != nil {
			t.Errorf("marshalInputSchema(nil) = %v, want nil", got)
		}
	})

	t.Run("object schema", func(t *testing.T) {
		raw, err := marshalInputSchema(objectSchema(map[string]any{
			"query": map[string]any{"type": "string"},
		}))
		if err != nil {
			t.Fatalf("marshalInputSchema() error = %v", err)
		}
		want := map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
			},
		}
		if !reflect.DeepEqual(decodeJSON(t, raw), want) {
			t.Errorf("marshalInputSchema() = %v, want %v", decodeJSON(t, raw), want)
		}
	})
}

func TestConvertCallToolResult(t *testing.T) {
	t.Run("text only", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hello"}},
		}
		text, binaries, metadata := convertCallToolResult(res)
		if text != "hello" {
			t.Errorf("text = %q, want %q", text, "hello")
		}
		if len(binaries) != 0 {
			t.Errorf("binaries = %v, want empty", binaries)
		}
		if metadata != nil {
			t.Errorf("metadata = %v, want nil", metadata)
		}
	})

	t.Run("multiple text blocks joined with newline", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.TextContent{Text: "first"},
				&sdkmcp.TextContent{Text: "second"},
			},
		}
		text, _, _ := convertCallToolResult(res)
		if text != "first\nsecond" {
			t.Errorf("text = %q, want %q", text, "first\nsecond")
		}
	})

	t.Run("binary and embedded resources", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			Content: []sdkmcp.Content{
				&sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte{0x89, 0x50}},
				&sdkmcp.AudioContent{MIMEType: "audio/mpeg", Data: []byte{0x01, 0x02}},
				&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{MIMEType: "text/csv", Blob: []byte("a,b,c")}},
				&sdkmcp.EmbeddedResource{Resource: &sdkmcp.ResourceContents{MIMEType: "text/plain", Text: "embedded text"}},
			},
		}
		text, binaries, _ := convertCallToolResult(res)
		if text != "embedded text" {
			t.Errorf("text = %q, want %q", text, "embedded text")
		}
		want := []tools.BinaryData{
			{MIMEType: "image/png", Data: []byte{0x89, 0x50}},
			{MIMEType: "audio/mpeg", Data: []byte{0x01, 0x02}},
			{MIMEType: "text/csv", Data: []byte("a,b,c")},
		}
		if !reflect.DeepEqual(binaries, want) {
			t.Errorf("binaries = %#v, want %#v", binaries, want)
		}
	})

	t.Run("structured content only", func(t *testing.T) {
		res := &sdkmcp.CallToolResult{
			StructuredContent: map[string]any{"result": "ok"},
		}
		_, _, metadata := convertCallToolResult(res)
		want := map[string]interface{}{
			"structuredContent": map[string]any{"result": "ok"},
		}
		if !reflect.DeepEqual(metadata, want) {
			t.Errorf("metadata = %#v, want %#v", metadata, want)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		text, binaries, metadata := convertCallToolResult(&sdkmcp.CallToolResult{})
		if text != "" || len(binaries) != 0 || metadata != nil {
			t.Errorf("got (%q, %v, %v), want empty", text, binaries, metadata)
		}
	})
}

func TestOperationContext(t *testing.T) {
	t.Run("applies configured timeout", func(t *testing.T) {
		c := &Client{timeout: 50 * time.Millisecond}
		ctx, cancel := c.operationContext(context.Background())
		defer cancel()

		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("expected a deadline")
		}
		if d := time.Until(dl); d <= 0 || d > 100*time.Millisecond {
			t.Errorf("deadline in %v, want between 0 and 100ms", d)
		}
	})

	t.Run("non-positive timeout returns original context", func(t *testing.T) {
		c := &Client{timeout: 0}
		orig := context.Background()
		ctx, cancel := c.operationContext(orig)
		defer cancel()
		if ctx != orig {
			t.Error("non-positive timeout should return the original context")
		}
	})
}

func TestListTools(t *testing.T) {
	t.Run("happy path with multiple tools", func(t *testing.T) {
		toolsList := []*sdkmcp.Tool{
			{
				Name:        "search",
				Description: "Search for things",
				InputSchema: objectSchema(map[string]any{
					"query": map[string]any{"type": "string"},
				}),
			},
			{
				Name:        "ping",
				Description: "Ping the server",
				InputSchema: objectSchema(nil),
			},
		}
		handlers := map[string]sdkmcp.ToolHandler{
			"search": func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{}, nil
			},
			"ping": func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{}, nil
			},
		}

		ts := newSDKTestServer(t, toolsList, handlers)
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		got, err := c.ListTools(context.Background())
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		got = sortDefs(got)

		if len(got) != 2 {
			t.Fatalf("len(ListTools()) = %d, want 2", len(got))
		}
		if got[0].Name != "ping" || got[1].Name != "search" {
			t.Errorf("names = [%q %q], want [ping search]", got[0].Name, got[1].Name)
		}
		if got[1].Description != "Search for things" {
			t.Errorf("search description = %q", got[1].Description)
		}
		wantSchema := map[string]any{
			"type":       "object",
			"properties": map[string]any{"query": map[string]any{"type": "string"}},
		}
		if !reflect.DeepEqual(decodeJSON(t, got[1].InputSchema), wantSchema) {
			t.Errorf("search InputSchema = %v, want %v", decodeJSON(t, got[1].InputSchema), wantSchema)
		}
	})

	t.Run("empty list", func(t *testing.T) {
		ts := newSDKTestServer(t, nil, map[string]sdkmcp.ToolHandler{})
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		got, err := c.ListTools(context.Background())
		if err != nil {
			t.Fatalf("ListTools() error = %v", err)
		}
		if len(got) != 0 {
			t.Errorf("len(ListTools()) = %d, want 0", len(got))
		}
	})

	t.Run("HTTP 500 error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		if _, err := c.ListTools(context.Background()); err == nil {
			t.Fatal("ListTools() error = nil, want error")
		}
	})
}

func TestCallTool(t *testing.T) {
	t.Run("text success", func(t *testing.T) {
		toolsList := []*sdkmcp.Tool{
			{Name: "echo", InputSchema: objectSchema(map[string]any{"text": map[string]any{"type": "string"}})},
		}
		handlers := map[string]sdkmcp.ToolHandler{
			"echo": func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "hello"}},
				}, nil
			},
		}

		ts := newSDKTestServer(t, toolsList, handlers)
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		res, err := c.CallTool(context.Background(), "echo", map[string]interface{}{"text": "hi"})
		if err != nil {
			t.Fatalf("CallTool() error = %v", err)
		}
		if res.Text != "hello" {
			t.Errorf("Text = %q, want %q", res.Text, "hello")
		}
		if res.Error != nil {
			t.Errorf("Error = %v, want nil", res.Error)
		}
	})

	t.Run("mixed text binary and structured metadata", func(t *testing.T) {
		toolsList := []*sdkmcp.Tool{
			{Name: "mixed", InputSchema: objectSchema(nil)},
		}
		handlers := map[string]sdkmcp.ToolHandler{
			"mixed": func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{
						&sdkmcp.TextContent{Text: "text output"},
						&sdkmcp.ImageContent{MIMEType: "image/png", Data: []byte{0x89, 0x50}},
						&sdkmcp.AudioContent{MIMEType: "audio/mpeg", Data: []byte{0x01, 0x02}},
					},
					StructuredContent: map[string]any{"result": "ok", "count": 3},
				}, nil
			},
		}

		ts := newSDKTestServer(t, toolsList, handlers)
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		res, err := c.CallTool(context.Background(), "mixed", nil)
		if err != nil {
			t.Fatalf("CallTool() error = %v", err)
		}
		if res.Error != nil {
			t.Fatalf("Error = %v, want nil", res.Error)
		}
		if res.Text != "text output" {
			t.Errorf("Text = %q, want %q", res.Text, "text output")
		}
		wantBinary := []tools.BinaryData{
			{MIMEType: "image/png", Data: []byte{0x89, 0x50}},
			{MIMEType: "audio/mpeg", Data: []byte{0x01, 0x02}},
		}
		if !reflect.DeepEqual(res.BinaryData, wantBinary) {
			t.Errorf("BinaryData = %#v, want %#v", res.BinaryData, wantBinary)
		}
		wantMeta := map[string]interface{}{
			"structuredContent": map[string]any{"result": "ok", "count": float64(3)},
		}
		if !reflect.DeepEqual(res.Metadata, wantMeta) {
			t.Errorf("Metadata = %#v, want %#v", res.Metadata, wantMeta)
		}
	})

	t.Run("MCP tool error is non-terminal", func(t *testing.T) {
		toolsList := []*sdkmcp.Tool{
			{Name: "fails", InputSchema: objectSchema(nil)},
		}
		handlers := map[string]sdkmcp.ToolHandler{
			"fails": func(ctx context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "something went wrong"}},
					IsError: true,
				}, nil
			},
		}

		ts := newSDKTestServer(t, toolsList, handlers)
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		res, err := c.CallTool(context.Background(), "fails", nil)
		if err != nil {
			t.Fatalf("CallTool() error = %v, want nil Go error for MCP tool error", err)
		}
		if res.Text != "something went wrong" {
			t.Errorf("Text = %q, want %q", res.Text, "something went wrong")
		}
		if res.Error == nil {
			t.Fatal("Error = nil, want non-nil")
		}
		if res.Error.Error() != "something went wrong" {
			t.Errorf("Error = %q, want %q", res.Error.Error(), "something went wrong")
		}
	})

	t.Run("unknown tool returns Go error", func(t *testing.T) {
		ts := newSDKTestServer(t, nil, map[string]sdkmcp.ToolHandler{})
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		if _, err := c.CallTool(context.Background(), "missing", nil); err == nil {
			t.Fatal("CallTool() error = nil, want error for unknown tool")
		}
	})

	t.Run("transport HTTP 500 returns Go error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer ts.Close()

		c, err := NewClient(ts.URL, "", 5*time.Second)
		if err != nil {
			t.Fatalf("NewClient() error = %v", err)
		}
		defer func() { _ = c.Close() }()

		if _, err := c.CallTool(context.Background(), "tool", nil); err == nil {
			t.Fatal("CallTool() error = nil, want transport error")
		}
	})
}

func TestCallToolContextCancellation(t *testing.T) {
	c, err := NewClient("http://example.com/mcp", "", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.CallTool(ctx, "tool", nil); err == nil {
		t.Fatal("CallTool() error = nil, want cancellation error")
	} else if !errors.Is(err, context.Canceled) {
		t.Errorf("CallTool() error = %v, want to wrap context.Canceled", err)
	}
}

func TestCallToolDeadlineExceeded(t *testing.T) {
	c, err := NewClient("http://example.com/mcp", "", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	if _, err := c.CallTool(ctx, "tool", nil); err == nil {
		t.Fatal("CallTool() error = nil, want deadline error")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("CallTool() error = %v, want to wrap context.DeadlineExceeded", err)
	}
}

func TestClose(t *testing.T) {
	c, err := NewClient("http://example.com/mcp", "", 5*time.Second)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	// Close before any connection is established is a no-op.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (unconnected) error = %v", err)
	}

	// Operations after Close must fail.
	if _, err := c.ListTools(context.Background()); err == nil {
		t.Fatal("ListTools() after Close = nil error, want closed error")
	} else if !strings.Contains(err.Error(), "closed") {
		t.Errorf("ListTools() after Close = %v, want closed error", err)
	}

	// Idempotent close.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (idempotent) error = %v", err)
	}
}

func TestCloseConnectedNoGoroutineLeak(t *testing.T) {
	defer goleak.VerifyNone(t,
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).readLoop"),
		goleak.IgnoreTopFunction("net/http.(*http2ClientConn).writeLoop"),
		goleak.IgnoreTopFunction("internal/poll.runtime_pollWait"),
	)

	toolsList := []*sdkmcp.Tool{
		{Name: "ping", InputSchema: objectSchema(nil)},
	}
	handlers := map[string]sdkmcp.ToolHandler{
		"ping": func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "pong"}}}, nil
		},
	}

	ts := newSDKTestServer(t, toolsList, handlers)

	// Disable keep-alives so no idle http.Transport connections survive past
	// server shutdown, keeping the leak check deterministic.
	c, err := NewClient(ts.URL, "", 5*time.Second,
		WithHTTPClient(&http.Client{Transport: &http.Transport{DisableKeepAlives: true}}),
	)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if _, err := c.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if _, err := c.CallTool(context.Background(), "ping", nil); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// Close is idempotent.
	if err := c.Close(); err != nil {
		t.Fatalf("Close() (second) error = %v", err)
	}

	ts.Close()
}
