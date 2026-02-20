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
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

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

func (t *networkTool) HttpRequest(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit reader to 5MB to prevent DoS
	limitReader := io.LimitReader(resp.Body, 5*1024*1024+1)
	respBody, err := io.ReadAll(limitReader)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %s\n", resp.Status))
	sb.WriteString("Headers:\n")
	for k, v := range resp.Header {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", k, strings.Join(v, ", ")))
	}
	sb.WriteString("\nBody:\n")

	respBodyStr := string(respBody)
	if len(respBodyStr) > 5*1024*1024 {
		respBodyStr = respBodyStr[:5*1024*1024] + "\n... (truncated due to size limit)"
	}
	sb.WriteString(respBodyStr)

	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *networkTool) ReadExternalDocs(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
	resp, err := t.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Limit reader to prevent DoS
	limitReader := io.LimitReader(resp.Body, 50001)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	// Basic HTML stripping
	// 1. Remove script and style tags and their contents
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	content = reStyle.ReplaceAllString(content, "")
	content = reScript.ReplaceAllString(content, "")

	// 2. Remove all other HTML tags
	reTags := regexp.MustCompile(`<.*?>`)
	content = reTags.ReplaceAllString(content, " ")

	// 3. Clean up whitespace
	reSpace := regexp.MustCompile(`\n\s*\n`)
	content = reSpace.ReplaceAllString(content, "\n\n")
	content = strings.Join(strings.Fields(content), " ")

	// Truncate to avoid huge inputs
	if len(content) > 10000 {
		content = content[:10000] + "\n... (truncated)"
	}

	return tools.ToolResult{Text: content}, nil
}
