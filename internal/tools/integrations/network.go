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
// is valid UTF-8 by removing bytes from the end until valid.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	s = s[:maxBytes]
	for len(s) > 0 {
		r, size := utf8.DecodeLastRuneInString(s)
		if r != utf8.RuneError || size == 3 {
			return s
		}
		s = s[:len(s)-1]
	}
	return ""
}

func (t *networkTool) sanitizeHTML(content string) string {
	var sb strings.Builder
	tokenizer := html.NewTokenizer(strings.NewReader(content))
	skip := 0
	for {
		tt := tokenizer.Next()
		if tt == html.ErrorToken {
			break
		}
		switch tt {
		case html.StartTagToken:
			tagName, _ := tokenizer.TagName()
			name := string(tagName)
			if name == "script" || name == "style" {
				skip++
			}
			sb.WriteByte(' ')
		case html.EndTagToken:
			tagName, _ := tokenizer.TagName()
			name := string(tagName)
			if name == "script" || name == "style" {
				if skip > 0 {
					skip--
				}
			}
			sb.WriteByte(' ')
		case html.TextToken:
			if skip == 0 {
				sb.Write(tokenizer.Text())
			}
		case html.SelfClosingTagToken:
			sb.WriteByte(' ')
		}
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
		content = truncateUTF8(content, 10000) + "\n... (truncated)"
	}

	return tools.ToolResult{Text: content}, nil
}
