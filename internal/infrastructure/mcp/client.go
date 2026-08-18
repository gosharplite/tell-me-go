// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package mcp implements the infrastructure adapter for the tools.MCPClient
// domain port using the Model Context Protocol (MCP) Go SDK. It exposes two
// transports: Streamable HTTP for remote servers (Client — the transport used
// by GitHub's hosted remote MCP server) and local stdio child processes
// (StdioClient — COMMAND + ARGS spawned as a subprocess). The SDK is isolated
// behind the domain port so it remains swappable (ADR-055/060 injection
// pattern).
package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// defaultTimeout is the per-operation timeout applied when the caller does not
// configure one (or configures a non-positive value).
const defaultTimeout = 300 * time.Second

// Client implements tools.MCPClient for a single MCP server over the
// Streamable HTTP transport.
type Client struct {
	endpoint   string
	token      string
	username   string
	timeout    time.Duration
	httpClient *http.Client

	mu      sync.Mutex
	sdk     *sdkmcp.Client
	session *sdkmcp.ClientSession
	closed  bool
}

// Compile-time assertion that Client satisfies the domain port.
var _ tools.MCPClient = (*Client)(nil)

// option configures a Client.
type option func(*Client)

// withHTTPClient overrides the *http.Client used by the underlying transport.
// It is useful for injecting a custom transport (for example, deterministic
// testing with httptest.Server or a keep-alive-free client).
func withHTTPClient(c *http.Client) option {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

// WithBasicAuth configures the client to attach an "Authorization: Basic
// base64(username:token)" header to every request, using the token passed
// positionally to NewClient as the Basic password. Exported because the DI
// factory (internal/infrastructure/di) is the production consumer.
func WithBasicAuth(username string) option {
	return func(cl *Client) {
		cl.username = username
	}
}

// NewClient constructs an MCP client adapter for the given endpoint.
//
// When token is non-empty, requests carry an "Authorization: Bearer <token>"
// header. When timeout is <= 0, it defaults to defaultTimeout.
func NewClient(endpoint, token string, timeout time.Duration, opts ...option) (*Client, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, errors.New("mcp: endpoint must not be empty")
	}
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	c := &Client{
		endpoint: endpoint,
		token:    token,
		timeout:  timeout,
	}
	for _, opt := range opts {
		opt(c)
	}

	c.sdk = sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "tell-me-go",
		Version: "dev",
	}, nil)

	return c, nil
}

// ListTools queries the MCP server for available tools.
func (c *Client) ListTools(ctx context.Context) ([]tools.MCPToolDefinition, error) {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()

	session, err := c.connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("mcp connect: %w", err)
	}

	res, err := session.ListTools(ctx, &sdkmcp.ListToolsParams{})
	if err != nil {
		return nil, fmt.Errorf("mcp list tools: %w", err)
	}

	defs := make([]tools.MCPToolDefinition, 0, len(res.Tools))
	for _, t := range res.Tools {
		raw, err := marshalInputSchema(t.InputSchema)
		if err != nil {
			return nil, fmt.Errorf("mcp list tools %q: %w", t.Name, err)
		}
		defs = append(defs, tools.MCPToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: raw,
		})
	}
	return defs, nil
}

// CallTool executes a tool on the remote MCP server with the provided arguments.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (tools.ToolResult, error) {
	ctx, cancel := c.operationContext(ctx)
	defer cancel()

	session, err := c.connect(ctx)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("mcp call %s: %w", name, err)
	}

	res, err := session.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      name,
		Arguments: args,
	})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("mcp call %s: %w", name, err)
	}

	text, binaries, metadata := convertCallToolResult(res)

	// Three-way split (ADR-022 / issue #1373): an MCP-level tool error is a
	// non-terminal domain outcome surfaced through ToolResult.Error with a nil
	// Go error, so the LLM can recover in-turn. Transport/HTTP/JSON-RPC errors
	// are terminal Go errors (returned above).
	if res.IsError {
		return tools.ToolResult{
			Text:     text,
			Error:    fmt.Errorf("%s", toolErrorSummary(name, text)),
			Metadata: metadata,
		}, nil
	}

	return tools.ToolResult{
		Text:       text,
		BinaryData: binaries,
		Metadata:   metadata,
	}, nil
}

// Close terminates the client connection and releases any underlying
// resources. It is idempotent and concurrency safe.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.session == nil {
		return nil
	}
	session := c.session
	c.session = nil
	return session.Close()
}

// connect establishes the MCP session lazily, at most once per client.
func (c *Client) connect(ctx context.Context) (*sdkmcp.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil, errors.New("mcp: client is closed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.session != nil {
		return c.session, nil
	}

	httpClient := c.httpClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}

	if c.username != "" {
		httpClient = withBasicAuthHeader(httpClient, c.username, c.token)
	} else {
		httpClient = withBearerToken(httpClient, c.token)
	}

	// A fresh transport per connect attempt: the SDK's transport may only be
	// connected once, so a failed handshake must not poison a later retry.
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint:             c.endpoint,
		HTTPClient:           httpClient,
		DisableStandaloneSSE: true,
	}

	session, err := c.sdk.Connect(ctx, transport, nil)
	if err != nil {
		return nil, err
	}
	c.session = session
	return session, nil
}

// operationContext applies the configured per-operation timeout to ctx.
func (c *Client) operationContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.timeout <= 0 {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}

// marshalInputSchema serializes an MCP tool's input schema to raw JSON.
func marshalInputSchema(schema any) (json.RawMessage, error) {
	if schema == nil {
		return nil, nil
	}
	b, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

// toolErrorSummary builds the non-terminal error message for an MCP-level
// tool failure.
func toolErrorSummary(name, text string) string {
	if strings.TrimSpace(text) != "" {
		return text
	}
	return fmt.Sprintf("tool %q returned an error", name)
}

// convertCallToolResult extracts the unstructured content blocks, binary
// items, and structured content from an MCP CallToolResult into the domain
// ToolResult representation.
func convertCallToolResult(res *sdkmcp.CallToolResult) (text string, binaries []tools.BinaryData, metadata map[string]interface{}) {
	var textParts []string

	for _, content := range res.Content {
		convertContentItem(content, &textParts, &binaries)
	}

	return strings.Join(textParts, "\n"), binaries, resultMetadata(res)
}

// convertContentItem dispatches a single MCP content block into the textParts
// and binaries accumulators. ResourceLink, ToolUseContent, ToolResultContent,
// and unknown content types carry no directly-relayable text/binary payload for
// the agent and are intentionally ignored (no default case is needed).
func convertContentItem(content sdkmcp.Content, textParts *[]string, binaries *[]tools.BinaryData) {
	switch c := content.(type) {
	case *sdkmcp.TextContent:
		if c != nil {
			*textParts = append(*textParts, c.Text)
		}
	case *sdkmcp.ImageContent:
		if c != nil {
			*binaries = append(*binaries, tools.BinaryData{MIMEType: c.MIMEType, Data: c.Data})
		}
	case *sdkmcp.AudioContent:
		if c != nil {
			*binaries = append(*binaries, tools.BinaryData{MIMEType: c.MIMEType, Data: c.Data})
		}
	case *sdkmcp.EmbeddedResource:
		convertEmbeddedResource(c, textParts, binaries)
	}
}

// convertEmbeddedResource handles the EmbeddedResource content kind, which may
// carry a binary blob, plain text, or both.
func convertEmbeddedResource(c *sdkmcp.EmbeddedResource, textParts *[]string, binaries *[]tools.BinaryData) {
	if c == nil || c.Resource == nil {
		return
	}
	if len(c.Resource.Blob) > 0 {
		*binaries = append(*binaries, tools.BinaryData{MIMEType: c.Resource.MIMEType, Data: c.Resource.Blob})
	}
	if c.Resource.Text != "" {
		*textParts = append(*textParts, c.Resource.Text)
	}
}

// resultMetadata captures the structured content into a fresh map for the
// domain ToolResult. Protocol-level _meta is intentionally dropped: it is
// transport plumbing (e.g. server identity), not tool output.
func resultMetadata(res *sdkmcp.CallToolResult) map[string]interface{} {
	if res.StructuredContent == nil {
		return nil
	}
	return map[string]interface{}{"structuredContent": res.StructuredContent}
}

// bearerTokenTransport injects an Authorization header into every request.
type bearerTokenTransport struct {
	base  http.RoundTripper
	token string
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

// withBearerToken returns a client that attaches the bearer token to every
// request. When token is empty, the original client is returned unchanged.
func withBearerToken(c *http.Client, token string) *http.Client {
	if token == "" {
		return c
	}

	cc := *c
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cc.Transport = &bearerTokenTransport{base: base, token: token}
	return &cc
}

// basicAuthTransport injects an "Authorization: Basic base64(user:token)"
// header into every request.
type basicAuthTransport struct {
	base     http.RoundTripper
	username string
	token    string
}

func (t *basicAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	if t.username != "" && t.token != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(t.username+":"+t.token)))
	}
	return t.base.RoundTrip(req)
}

// withBasicAuthHeader returns a client that attaches the Basic credentials
// to every request. When username or token is empty, the original client is
// returned unchanged.
func withBasicAuthHeader(c *http.Client, username, token string) *http.Client {
	if username == "" || token == "" {
		return c
	}
	cc := *c
	base := c.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	cc.Transport = &basicAuthTransport{base: base, username: username, token: token}
	return &cc
}
