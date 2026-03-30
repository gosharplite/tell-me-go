// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var (
	styleRegex  *regexp.Regexp
	scriptRegex *regexp.Regexp
	tagsRegex   *regexp.Regexp
	spaceRegex  *regexp.Regexp
	regexOnce   sync.Once
)

func initRegex() {
	styleRegex = regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	scriptRegex = regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	tagsRegex = regexp.MustCompile(`<.*?>`)
	spaceRegex = regexp.MustCompile(`\n\s*\n`)
}

type networkTool struct {
	sm     security.TerminalController
	client tools.HTTPClient
}

func newnetworkTool(sm security.TerminalController, client tools.HTTPClient) *networkTool {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &networkTool{sm: sm, client: client}
}

func (t *networkTool) startHeartbeat(hb chan<- struct{}) (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
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
		close(done)
	}
}

func (t *networkTool) readResponseWithLimit(body io.ReadCloser, limit int64) (string, bool, error) {
	limitReader := io.LimitReader(body, limit+1)
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return "", false, fmt.Errorf("failed to read response: %w", err)
	}
	truncated := len(data) > int(limit)
	content := string(data)
	if truncated {
		content = content[:limit]
	}
	return content, truncated, nil
}

func (t *networkTool) sanitizeHTML(content string) string {
	regexOnce.Do(initRegex)
	content = styleRegex.ReplaceAllString(content, "")
	content = scriptRegex.ReplaceAllString(content, "")
	content = tagsRegex.ReplaceAllString(content, " ")
	content = spaceRegex.ReplaceAllString(content, "\n\n")
	content = strings.Join(strings.Fields(content), " ")
	return content
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

	method := params.Method
	url := params.URL
	bodyStr := params.Body

	t.sm.Warn(fmt.Sprintf("[Tool Action] HTTP %s %s", method, url))

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	stopHB := t.startHeartbeat(hb)
	defer stopHB()

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const limit = 5 * 1024 * 1024
	bodyContent, truncated, err := t.readResponseWithLimit(resp.Body, limit)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if truncated {
		bodyContent += "\n... (truncated due to size limit)"
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Status: %s\n", resp.Status)
	sb.WriteString("Headers:\n")
	for k, v := range resp.Header {
		_, _ = fmt.Fprintf(&sb, "  %s: %s\n", k, strings.Join(v, ", "))
	}
	sb.WriteString("\nBody:\n")
	sb.WriteString(bodyContent)

	return tools.ToolResult{Text: sb.String()}, nil
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

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)

	stopHB := t.startHeartbeat(hb)
	defer stopHB()

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	const limit = 50000
	bodyContent, _, err := t.readResponseWithLimit(resp.Body, limit)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	content := t.sanitizeHTML(bodyContent)

	// Truncate to avoid huge inputs
	if len(content) > 10000 {
		content = content[:10000] + "\n... (truncated)"
	}

	return tools.ToolResult{Text: content}, nil
}
