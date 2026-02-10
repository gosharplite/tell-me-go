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

const defaultConfluenceBaseURL = "https://02007.atlassian.net"

var (
	reH1          = regexp.MustCompile(`(?i)<h1.*?>`)
	reH2          = regexp.MustCompile(`(?i)<h2.*?>`)
	reH3          = regexp.MustCompile(`(?i)<h3.*?>`)
	reH4          = regexp.MustCompile(`(?i)<h4.*?>`)
	reH5          = regexp.MustCompile(`(?i)<h5.*?>`)
	reH6          = regexp.MustCompile(`(?i)<h6.*?>`)
	reCloseHeader = regexp.MustCompile(`(?i)</h[1-6]>`)
	reLi          = regexp.MustCompile(`(?i)<li.*?>`)
	reCloseLi     = regexp.MustCompile(`(?i)</li>`)
	reBr          = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlocks      = regexp.MustCompile(`(?i)</?(p|div|tr|td|table|ul|ol).*?>`)
	reTags        = regexp.MustCompile(`<.*?>`)
	reMultiSpace  = regexp.MustCompile(` +`)
	reLeadingSpace  = regexp.MustCompile(`(?m)^ +`)
	reTrailingSpace = regexp.MustCompile(`(?m) +$`)
	reMultiNewline  = regexp.MustCompile(`\n\n+`)

	reBold1   = regexp.MustCompile(`\*\*(.*?)\*\*`)
	reBold2   = regexp.MustCompile(`__(.*?)__`)
	reItalic1 = regexp.MustCompile(`\*(.*?)\*`)
	reItalic2 = regexp.MustCompile(`_(.*?)_`)
)

type confluenceManager struct {
	sm      *security.SecurityManager
	client  tools.HTTPClient
	baseURL string
}

// NewConfluenceManager creates a new instance of confluenceManager.
func NewConfluenceManager(sm *security.SecurityManager, client tools.HTTPClient) *confluenceManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	baseURL := os.Getenv("ATLASSIAN_BASE_URL")
	if baseURL == "" {
		// Use default fallback if environment variable is missing
		baseURL = defaultConfluenceBaseURL
	}
	return &confluenceManager{
		sm:      sm,
		client:  client,
		baseURL: baseURL,
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
		Limit   int    `json:"limit"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Title != "" && params.SpaceID == "" {
		return tools.ToolResult{}, fmt.Errorf("space_id is required for fuzzy keyword search")
	}

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	apiURL := fmt.Sprintf("%s/wiki/api/v2/pages", m.baseURL)
	u, err := url.Parse(apiURL)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse Confluence API URL: %w", err)
	}

	q := u.Query()
	if params.SpaceID != "" {
		q.Set("space-id", params.SpaceID)
	}
	q.Set("limit", "50")
	q.Set("sort", "-modified-date")
	u.RawQuery = q.Encode()

	nextURL := u.String()
	var matches []struct {
		ID    string
		Title string
	}

	maxPages := params.Limit
	if maxPages <= 0 {
		maxPages = 250
	}
	warning := ""
	if maxPages > 1000 {
		maxPages = 1000
		warning = "Note: Search depth capped at 1000 pages for safety.\n\n"
	}

	maxIterations := (maxPages + 49) / 50
	keyword := strings.ToLower(params.Title)
	pagesProcessed := 0

	for i := 0; i < maxIterations; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, nextURL, nil)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Accept", "application/json")

		resp, err := m.client.Do(req)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			return tools.ToolResult{}, fmt.Errorf("Confluence API returned status: %s, body: %s", resp.Status, string(body))
		}

		var responseData struct {
			Results []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"results"`
			Links struct {
				Next string `json:"next"`
			} `json:"_links"`
		}

		err = json.NewDecoder(resp.Body).Decode(&responseData)
		resp.Body.Close()
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, page := range responseData.Results {
			pagesProcessed++
			if keyword == "" || strings.Contains(strings.ToLower(page.Title), keyword) {
				matches = append(matches, struct {
					ID    string
					Title string
				}{ID: page.ID, Title: page.Title})
			}
		}

		if responseData.Links.Next == "" {
			break
		}

		// Handle absolute vs relative Next link
		if strings.HasPrefix(responseData.Links.Next, "http") {
			nextURL = responseData.Links.Next
		} else {
			parsedBase, _ := url.Parse(m.baseURL)
			parsedNext, err := url.Parse(responseData.Links.Next)
			if err != nil {
				break
			}
			nextURL = parsedBase.ResolveReference(parsedNext).String()
		}
	}

	if len(matches) == 0 {
		if params.Title != "" {
			return tools.ToolResult{Text: fmt.Sprintf("%sSearched the %d most recently modified pages in space '%s' but found no pages containing '%s'. Please try an exact title or a different keyword.", warning, pagesProcessed, params.SpaceID, params.Title)}, nil
		}
		return tools.ToolResult{Text: warning + "No pages found matching the criteria."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(warning)
	resultText.WriteString("Found pages:\n")
	for _, page := range matches {
		resultText.WriteString(fmt.Sprintf("- %s (ID: %s)\n", page.Title, page.ID))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
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

	apiURL := fmt.Sprintf("%s/wiki/api/v2/pages/%s?body-format=storage", m.baseURL, params.PageID)
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

	const maxPageSize = 5 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxPageSize)+1))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	if len(body) > maxPageSize {
		return tools.ToolResult{}, fmt.Errorf("Confluence page size exceeds the 5MB limit")
	}

	var responseData struct {
		Title string `json:"title"`
		Body  struct {
			Storage struct {
				Value string `json:"value"`
			} `json:"storage"`
		} `json:"body"`
	}

	if err := json.Unmarshal(body, &responseData); err != nil {
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
	content = reH1.ReplaceAllString(content, "\n\n# ")
	content = reH2.ReplaceAllString(content, "\n\n## ")
	content = reH3.ReplaceAllString(content, "\n\n### ")
	content = reH4.ReplaceAllString(content, "\n\n#### ")
	content = reH5.ReplaceAllString(content, "\n\n##### ")
	content = reH6.ReplaceAllString(content, "\n\n###### ")
	content = reCloseHeader.ReplaceAllString(content, "\n\n")

	// 3. Handle lists
	content = reLi.ReplaceAllString(content, "\n* ")
	content = reCloseLi.ReplaceAllString(content, "")

	// 4. Handle line breaks and block elements
	content = reBr.ReplaceAllString(content, "\n")
	content = reBlocks.ReplaceAllString(content, "\n\n")

	// 5. Strip all remaining HTML tags
	content = reTags.ReplaceAllString(content, "")

	// 6. Unescape HTML entities
	content = html.UnescapeString(content)
	// Replace &nbsp; (\u00a0) with regular space for test compatibility
	content = strings.ReplaceAll(content, "\u00a0", " ")

	// 7. Clean up whitespace
	// Multi-space to single space
	content = reMultiSpace.ReplaceAllString(content, " ")

	// Remove leading/trailing spaces on each line
	content = reLeadingSpace.ReplaceAllString(content, "")
	content = reTrailingSpace.ReplaceAllString(content, "")

	// Fix multiple newlines
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
	apiURL := fmt.Sprintf("%s/wiki/api/v2/pages/%s", m.baseURL, params.PageID)
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
			content := html.EscapeString(strings.TrimPrefix(line, "# "))
			result.WriteString("<h1>" + content + "</h1>")
		} else if strings.HasPrefix(line, "## ") {
			content := html.EscapeString(strings.TrimPrefix(line, "## "))
			result.WriteString("<h2>" + content + "</h2>")
		} else if strings.HasPrefix(line, "### ") {
			content := html.EscapeString(strings.TrimPrefix(line, "### "))
			result.WriteString("<h3>" + content + "</h3>")
		} else if strings.HasPrefix(line, "#### ") {
			content := html.EscapeString(strings.TrimPrefix(line, "#### "))
			result.WriteString("<h4>" + content + "</h4>")
		} else if strings.HasPrefix(line, "##### ") {
			content := html.EscapeString(strings.TrimPrefix(line, "##### "))
			result.WriteString("<h5>" + content + "</h5>")
		} else if strings.HasPrefix(line, "###### ") {
			content := html.EscapeString(strings.TrimPrefix(line, "###### "))
			result.WriteString("<h6>" + content + "</h6>")
		} else {
			// Escape first to avoid escaping generated tags
			line = html.EscapeString(line)
			// Apply inline formatting
			line = reBold1.ReplaceAllString(line, "<b>$1</b>")
			line = reBold2.ReplaceAllString(line, "<b>$1</b>")
			line = reItalic1.ReplaceAllString(line, "<i>$1</i>")
			line = reItalic2.ReplaceAllString(line, "<i>$1</i>")
			result.WriteString("<p>" + line + "</p>")
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
