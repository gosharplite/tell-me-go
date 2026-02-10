// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"bytes"
	"html"
	"io"
	"regexp"
	"strings"
)

type confluenceManager struct {
	sm     *security.SecurityManager
	client tools.HTTPClient
}

// NewConfluenceManager creates a new instance of confluenceManager.
func NewConfluenceManager(sm *security.SecurityManager, client tools.HTTPClient) *confluenceManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &confluenceManager{
		sm:     sm,
		client: client,
	}
}

func (m *confluenceManager) getAuthHeader() (string, error) {
	email := os.Getenv("ATLASSIAN_EMAIL")
	if email == "" {
		return "", fmt.Errorf("Missing ATLASSIAN_EMAIL environment variable")
	}
	token := os.Getenv("ATLASSIAN_TOKEN")
	if token == "" {
		return "", fmt.Errorf("Missing ATLASSIAN_TOKEN environment variable")
	}

	auth := fmt.Sprintf("%s:%s", email, token)
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	return fmt.Sprintf("Basic %s", encoded), nil
}

func (m *confluenceManager) confluenceSearch(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Title   string `json:"title"`
		SpaceID string `json:"space_id"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	apiURL := "https://02007.atlassian.net/wiki/api/v2/pages"
	u, err := url.Parse(apiURL)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse Confluence API URL: %w", err)
	}

	q := u.Query()
	if params.Title != "" {
		q.Set("title", params.Title)
	}
	if params.SpaceID != "" {
		q.Set("space-id", params.SpaceID)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("Confluence API returned status: %s", resp.Status)
	}

	var responseData struct {
		Results []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"results"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Results) == 0 {
		return tools.ToolResult{Text: "No pages found matching the criteria."}, nil
	}

	var resultText string
	resultText = "Found pages:\n"
	for _, page := range responseData.Results {
		resultText += fmt.Sprintf("- %s (ID: %s)\n", page.Title, page.ID)
	}

	return tools.ToolResult{Text: resultText}, nil
}

func (m *confluenceManager) confluenceRead(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		PageID string `json:"page_id"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.PageID == "" {
		return tools.ToolResult{}, fmt.Errorf("page_id argument is required")
	}

	apiURL := fmt.Sprintf("https://02007.atlassian.net/wiki/api/v2/pages/%s?body-format=storage", params.PageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return tools.ToolResult{}, fmt.Errorf("Confluence page not found: %s", params.PageID)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return tools.ToolResult{}, fmt.Errorf("authentication failed: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("Confluence API returned status: %s", resp.Status)
	}

	// Limit reader to 5MB
	limitReader := io.LimitReader(resp.Body, 5*1024*1024+1)
	var responseData struct {
		Title string `json:"title"`
		Body  struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}

	if err := json.NewDecoder(limitReader).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	markdown := m.xhtmlToMarkdown(responseData.Body.Storage.Value)
	resultText := fmt.Sprintf("# %s\n\n%s", responseData.Title, markdown)

	return tools.ToolResult{Text: resultText}, nil
}

func (m *confluenceManager) xhtmlToMarkdown(xhtml string) string {
	// 1. Normalize whitespace - replace all newlines and tabs with spaces
	content := strings.ReplaceAll(xhtml, "\r", "")
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\t", " ")

	// 2. Handle headers
	reH1 := regexp.MustCompile(`(?i)<h1.*?>`)
	reH2 := regexp.MustCompile(`(?i)<h2.*?>`)
	reH3 := regexp.MustCompile(`(?i)<h3.*?>`)
	reH4 := regexp.MustCompile(`(?i)<h4.*?>`)
	reH5 := regexp.MustCompile(`(?i)<h5.*?>`)
	reH6 := regexp.MustCompile(`(?i)<h6.*?>`)
	reCloseHeader := regexp.MustCompile(`(?i)</h[1-6]>`)

	content = reH1.ReplaceAllString(content, "\n\n# ")
	content = reH2.ReplaceAllString(content, "\n\n## ")
	content = reH3.ReplaceAllString(content, "\n\n### ")
	content = reH4.ReplaceAllString(content, "\n\n#### ")
	content = reH5.ReplaceAllString(content, "\n\n##### ")
	content = reH6.ReplaceAllString(content, "\n\n###### ")
	content = reCloseHeader.ReplaceAllString(content, "\n\n")

	// 3. Handle lists
	reLi := regexp.MustCompile(`(?i)<li.*?>`)
	reCloseLi := regexp.MustCompile(`(?i)</li>`)
	content = reLi.ReplaceAllString(content, "\n* ")
	content = reCloseLi.ReplaceAllString(content, "")

	// 4. Handle line breaks and block elements
	reBr := regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlocks := regexp.MustCompile(`(?i)</?(p|div|tr|td|table|ul|ol).*?>`)

	content = reBr.ReplaceAllString(content, "\n")
	content = reBlocks.ReplaceAllString(content, "\n\n")

	// 5. Strip all remaining HTML tags
	reTags := regexp.MustCompile(`<.*?>`)
	content = reTags.ReplaceAllString(content, "")

	// 6. Unescape HTML entities
	content = html.UnescapeString(content)

	// 7. Clean up whitespace
	// Multi-space to single space
	reMultiSpace := regexp.MustCompile(` +`)
	content = reMultiSpace.ReplaceAllString(content, " ")

	// Remove leading/trailing spaces on each line
	reLeadingSpace := regexp.MustCompile(`(?m)^ +`)
	content = reLeadingSpace.ReplaceAllString(content, "")
	reTrailingSpace := regexp.MustCompile(`(?m) +$`)
	content = reTrailingSpace.ReplaceAllString(content, "")

	// Fix multiple newlines
	reMultiNewline := regexp.MustCompile(`\n\n+`)
	content = reMultiNewline.ReplaceAllString(content, "\n\n")

	return strings.TrimSpace(content)
}

func (m *confluenceManager) confluenceWrite(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		PageID          string `json:"page_id"`
		Title           string `json:"title"`
		MarkdownContent string `json:"markdown_content"`
		UpdateMessage   string `json:"update_message"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.PageID == "" || params.MarkdownContent == "" {
		return tools.ToolResult{}, fmt.Errorf("page_id and markdown_content are required")
	}

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 1. Fetch Current Version
	apiURL := fmt.Sprintf("https://02007.atlassian.net/wiki/api/v2/pages/%s", params.PageID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create version fetch request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("version fetch request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("failed to fetch current version, status: %s", resp.Status)
	}

	var currentData struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Version struct {
			Number int `json:"number"`
		} `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&currentData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode current page data: %w", err)
	}

	// 2. Prepare Update
	newTitle := params.Title
	if newTitle == "" {
		newTitle = currentData.Title
	}
	nextVersion := currentData.Version.Number + 1
	xhtml := m.markdownToXhtml(params.MarkdownContent)

	// 3. Security Confirmation
	detail := fmt.Sprintf("Update Message: %s\n\nNew Content Snippet:\n%s", params.UpdateMessage, truncate(params.MarkdownContent, 200))
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "update Confluence page", params.PageID, detail)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action cancelled by user."}, nil
	}

	// 4. Execute Update
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

	bodyBytes, err := json.Marshal(updatePayload)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to marshal update payload: %w", err)
	}

	req, err = http.NewRequestWithContext(ctx, http.MethodPut, apiURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create update request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err = m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("update request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return tools.ToolResult{Text: "Conflict: The page version has changed during the update process. Please retry."}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{}, fmt.Errorf("update failed with status: %s", resp.Status)
	}

	return tools.ToolResult{Text: fmt.Sprintf("Successfully updated Confluence page %s to version %d.", params.PageID, nextVersion)}, nil
}

func (m *confluenceManager) markdownToXhtml(markdown string) string {
	lines := strings.Split(markdown, "\n")
	var result strings.Builder
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			result.WriteString("<h1>" + html.EscapeString(strings.TrimPrefix(line, "# ")) + "</h1>")
		} else if strings.HasPrefix(line, "## ") {
			result.WriteString("<h2>" + html.EscapeString(strings.TrimPrefix(line, "## ")) + "</h2>")
		} else if strings.HasPrefix(line, "### ") {
			result.WriteString("<h3>" + html.EscapeString(strings.TrimPrefix(line, "### ")) + "</h3>")
		} else if strings.HasPrefix(line, "#### ") {
			result.WriteString("<h4>" + html.EscapeString(strings.TrimPrefix(line, "#### ")) + "</h4>")
		} else if strings.HasPrefix(line, "##### ") {
			result.WriteString("<h5>" + html.EscapeString(strings.TrimPrefix(line, "##### ")) + "</h5>")
		} else if strings.HasPrefix(line, "###### ") {
			result.WriteString("<h6>" + html.EscapeString(strings.TrimPrefix(line, "###### ")) + "</h6>")
		} else {
			result.WriteString("<p>" + html.EscapeString(line) + "</p>")
		}
	}
	return result.String()
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
