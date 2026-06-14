// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"bytes"
	"io"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type ConfluenceManager struct {
	sm       confluenceSecurity
	client   tools.HTTPClient
	provider *AtlassianProvider
}

// NewConfluenceManager creates a new instance of ConfluenceManager.
func NewConfluenceManager(sm confluenceSecurity, client tools.HTTPClient) (*ConfluenceManager, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	provider, err := NewAtlassianProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Atlassian provider: %w", err)
	}
	return &ConfluenceManager{
		sm:       sm,
		client:   client,
		provider: provider,
	}, nil
}

type pageVersionInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

func (m *ConfluenceManager) fetchPageContent(ctx context.Context, pageID string) (*http.Response, error) {
	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/wiki/api/v2/pages", pageID)
	q := u.Query()
	q.Set("body-format", "storage")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		_ = drainAndClose(resp.Body) // Drain to enable connection reuse; close errors on error paths are non-actionable.
		return nil, fmt.Errorf("confluence page not found: %s", pageID)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = drainAndClose(resp.Body) // Drain to enable connection reuse; close errors on error paths are non-actionable.
		return nil, fmt.Errorf("authentication failed: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		_ = drainAndClose(resp.Body) // Drain to enable connection reuse; close errors on error paths are non-actionable.
		return nil, fmt.Errorf("confluence API returned status: %s", resp.Status)
	}

	return resp, nil
}

func (m *ConfluenceManager) readAndValidateBody(respBody io.ReadCloser) ([]byte, error) {
	defer func() { _ = respBody.Close() }() // body fully consumed by io.ReadAll above; close errors are non-actionable
	const maxPageSize = 5 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(respBody, int64(maxPageSize)+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if len(body) > maxPageSize {
		return nil, fmt.Errorf("confluence page size exceeds the 5MB limit")
	}
	return body, nil
}

func (m *ConfluenceManager) decodePageResponse(body []byte) (title, storageValue string, err error) {
	var responseData struct {
		Title string `json:"title"`
		Body  struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}

	if err := json.Unmarshal(body, &responseData); err != nil {
		return "", "", fmt.Errorf("failed to decode response: %w", err)
	}
	return responseData.Title, responseData.Body.Storage.Value, nil
}

func (m *ConfluenceManager) ConfluenceRead(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		PageID string `json:"page_id"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.PageID == "" {
		return tools.ToolResult{}, fmt.Errorf("page_id argument is required")
	}

	resp, err := m.fetchPageContent(ctx, params.PageID)
	if err != nil {
		return tools.ToolResult{}, err
	}

	body, err := m.readAndValidateBody(resp.Body)
	if err != nil {
		return tools.ToolResult{}, err
	}

	title, storageValue, err := m.decodePageResponse(body)
	if err != nil {
		return tools.ToolResult{}, err
	}

	markdown := m.xhtmlToMarkdown(storageValue)
	resultText := fmt.Sprintf("# %s\n\n%s", title, markdown)

	return tools.ToolResult{Text: resultText}, nil
}

func (m *ConfluenceManager) getCurrentPageVersion(ctx context.Context, pageID string) (*pageVersionInfo, error) {
	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/wiki/api/v2/pages", pageID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create version fetch request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return nil, fmt.Errorf("version fetch request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // body consumed by json.Decoder; close error non-actionable

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch current version, status: %s", resp.Status)
	}

	var currentData pageVersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&currentData); err != nil {
		return nil, fmt.Errorf("failed to decode current page data: %w", err)
	}
	return &currentData, nil
}

func (m *ConfluenceManager) executeUpdate(ctx context.Context, pageID string, payload map[string]interface{}) error {
	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/wiki/api/v2/pages", pageID)

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal update payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewBuffer(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create update request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return fmt.Errorf("update request failed: %w", err)
	}
	defer func() {
		_ = drainAndClose(resp.Body) // Write path: drain to enable connection reuse
	}()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("conflict: page version changed during update; please retry")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update failed with status: %s", resp.Status)
	}

	return nil
}

func (m *ConfluenceManager) confluenceWrite(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		PageID          string `json:"page_id"`
		Title           string `json:"title"`
		MarkdownContent string `json:"markdown_content"`
		UpdateMessage   string `json:"update_message"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.PageID == "" || params.MarkdownContent == "" {
		return tools.ToolResult{}, fmt.Errorf("page_id and markdown_content are required")
	}

	currentData, err := m.getCurrentPageVersion(ctx, params.PageID)
	if err != nil {
		return tools.ToolResult{}, err
	}

	newTitle := params.Title
	if newTitle == "" {
		newTitle = currentData.Title
	}
	nextVersion := currentData.Version.Number + 1
	xhtml := m.markdownToXhtml(params.MarkdownContent)

	updatePayload := map[string]interface{}{
		"id":     params.PageID,
		"status": "current",
		"title":  newTitle,
		"body": map[string]interface{}{
			"representation": "storage",
			"value":          xhtml,
		},
		"version": map[string]interface{}{
			"number":  nextVersion,
			"message": params.UpdateMessage,
		},
	}

	if err := m.executeUpdate(ctx, params.PageID, updatePayload); err != nil {
		if strings.Contains(err.Error(), "conflict:") {
			return tools.ToolResult{Text: err.Error()}, nil
		}
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: fmt.Sprintf("Successfully updated Confluence page %s to version %d.", params.PageID, nextVersion)}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// drainAndClose drains any remaining bytes from the response body to enable
// connection reuse, then closes it. The close error is returned; drain errors
// are discarded (they are always expected when draining after a partial read).
func drainAndClose(body io.ReadCloser) error {
	_, _ = io.Copy(io.Discard, body)
	return body.Close()
}

type confluenceSecurity interface {
	security.ActionConfirmer
}
