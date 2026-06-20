// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"golang.org/x/net/html"
)

const (
	maxHttpRequestSize = 5 * 1024 * 1024
	maxExternalDocSize = 50000
	truncatedDocSize   = 10000
)

type networkTool struct {
	sm                security.TerminalController
	client            tools.HTTPClient
	heartbeatInterval time.Duration
}

func newnetworkTool(sm security.TerminalController, client tools.HTTPClient) *networkTool {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &networkTool{
		sm:                sm,
		client:            client,
		heartbeatInterval: 2 * time.Second,
	}
}

func (t *networkTool) startHeartbeat(hb chan<- struct{}) (stop func()) {
	done := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	interval := t.heartbeatInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	go func() {
		defer close(exited)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	return func() {
		once.Do(func() {
			close(done)
		})
		<-exited
	}
}

// executeRequest creates and executes an HTTP request with heartbeat, returning
// the response, heartbeat stop function, and any error.
func (t *networkTool) executeRequest(ctx context.Context, method, url, bodyStr string, headers map[string]string, hb chan<- struct{}) (*http.Response, func(), error) {
	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}

	t.sm.Warn(fmt.Sprintf("[Tool Action] HTTP %s %s", method, url))

	var stopHB func()
	if hb != nil {
		stopHB = t.startHeartbeat(hb)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		if stopHB != nil {
			stopHB()
		}
		return nil, nil, fmt.Errorf("request failed: %w", err)
	}
	return resp, stopHB, nil
}

func (t *networkTool) readResponseWithLimit(r io.Reader, limit int64) (string, bool, error) {
	limitReader := io.LimitReader(r, limit+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}
	truncated := len(data) > int(limit)
	content := string(data)
	if truncated {
		// Safe truncation that preserves UTF-8 validity
		content = truncateUTF8(content, int(limit))
	}
	return content, truncated, nil
}

// truncateUTF8 truncates a string to at most maxBytes bytes, ensuring the result
// is valid UTF-8 and does not split a multi-byte rune.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) > maxBytes {
		// Backtrack to the start of a rune to avoid splitting a character boundary
		for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
			maxBytes--
		}
		s = s[:maxBytes]
	}
	// Clean any internal invalid UTF-8 bytes in O(N) time without discarding the whole string
	return strings.ToValidUTF8(s, "")
}

// drainAndClose drains remaining bytes from a response body to enable HTTP
// connection reuse, then closes it. The close error is returned; drain errors
// are discarded (they are expected when draining after a partial read).
func drainAndClose(body io.ReadCloser) error {
	_, _ = io.Copy(io.Discard, body)
	return body.Close()
}

// mustFprintf is fmt.Fprintf for strings.Builder, which never returns an error.
// It eliminates the noisy _, _ = pattern at call sites.
func mustFprintf(sb *strings.Builder, format string, args ...interface{}) {
	_, _ = fmt.Fprintf(sb, format, args...)
}

// formatHTTPResponse builds the formatted HTTP response string.
// It is a pure function with no I/O dependencies.
func formatHTTPResponse(status string, headers http.Header, body string, truncated bool) string {
	var sb strings.Builder
	mustFprintf(&sb, "Status: %s\n", status)
	sb.WriteString("Headers:\n")
	for k, vs := range headers {
		mustFprintf(&sb, "  %s: %s\n", k, strings.Join(vs, ", "))
	}
	sb.WriteString("\nBody:\n")
	sb.WriteString(body)
	if truncated {
		sb.WriteString("\n... (truncated due to size limit)")
	}
	return sb.String()
}

// handleHTMLToken processes a single HTML token, updating the skip counter
// for script/style suppression and writing text to sb.
func (t *networkTool) handleHTMLToken(tok html.Token, skip *int, sb *strings.Builder) {
	if tok.Type == html.TextToken {
		if *skip == 0 {
			sb.WriteString(tok.Data)
		}
		return
	}
	sb.WriteByte(' ')
	if tok.Data == "script" || tok.Data == "style" {
		if tok.Type == html.StartTagToken {
			*skip++
		} else if tok.Type == html.EndTagToken && *skip > 0 {
			*skip--
		}
	}
}

func (t *networkTool) sanitizeHTML(content string) string {
	var sb strings.Builder
	z := html.NewTokenizer(strings.NewReader(content))
	skip := 0
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		t.handleHTMLToken(z.Token(), &skip, &sb)
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}

func (t *networkTool) HttpRequest(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resp, _, err := t.executeRequest(ctx, params.Method, params.URL, params.Body, params.Headers, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer func() { _ = drainAndClose(resp.Body) }()

	bodyContent, truncated, err := t.readResponseWithLimit(resp.Body, maxHttpRequestSize)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: formatHTTPResponse(resp.Status, resp.Header, bodyContent, truncated)}, nil
}

func (t *networkTool) ReadExternalDocs(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	url := params.URL
	if url == "" {
		return tools.ToolResult{}, fmt.Errorf("url argument is required")
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request for url %q: %w", url, err)
	}

	stopHB := t.startHeartbeat(hb)
	defer stopHB()

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() { _ = drainAndClose(resp.Body) }()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	bodyContent, _, err := t.readResponseWithLimit(resp.Body, maxExternalDocSize)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	content := t.sanitizeHTML(bodyContent)

	// Truncate to avoid huge inputs
	if len(content) > truncatedDocSize {
		content = truncateUTF8(content, truncatedDocSize) + "\n... (truncated)"
	}

	return tools.ToolResult{Text: content}, nil
}

func registerNetwork(r tools.Registry, net *networkTool) error {
	if err := r.RegisterToToolkitWithOptions("network", &tools.ToolDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches and cleans content from a URL, stripping HTML tags and scripts to provide readable documentation. Useful for researching library APIs.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"url": {
					Type:        "STRING",
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, net.ReadExternalDocs, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterToToolkitWithOptions("network", &tools.ToolDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"method": {
					Type:        "STRING",
					Description: "HTTP method (GET, POST, PUT, DELETE, etc.).",
				},
				"url": {
					Type:        "STRING",
					Description: "The target URL.",
				},
				"headers": {
					Type:        "OBJECT",
					Description: "HTTP headers as a map of strings.",
					Properties: map[string]*tools.Schema{
						"Content-Type": {Type: "STRING"},
					},
				},
				"body": {
					Type:        "STRING",
					Description: "Request body content.",
				},
			},
			Required: []string{"method", "url"},
		},
	}, net.HttpRequest, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}
	return nil
}
