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
	"html"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var (
	reH1            = regexp.MustCompile(`(?i)<h1.*?>`)
	reH2            = regexp.MustCompile(`(?i)<h2.*?>`)
	reH3            = regexp.MustCompile(`(?i)<h3.*?>`)
	reH4            = regexp.MustCompile(`(?i)<h4.*?>`)
	reH5            = regexp.MustCompile(`(?i)<h5.*?>`)
	reH6            = regexp.MustCompile(`(?i)<h6.*?>`)
	reCloseHeader   = regexp.MustCompile(`(?i)</h[1-6]>`)
	reLi            = regexp.MustCompile(`(?i)<li.*?>`)
	reCloseLi       = regexp.MustCompile(`(?i)</li>`)
	reBr            = regexp.MustCompile(`(?i)<br\s*/?>`)
	reBlocks        = regexp.MustCompile(`(?i)</?(p|div|tr|td|table|ul|ol).*?>`)
	reTags          = regexp.MustCompile(`<.*?>`)
	reMultiSpace    = regexp.MustCompile(` +`)
	reLeadingSpace  = regexp.MustCompile(`(?m)^ +`)
	reTrailingSpace = regexp.MustCompile(`(?m) +$`)
	reMultiNewline  = regexp.MustCompile(`\n\n+`)

	reBold1   = regexp.MustCompile(`\*\*(.*?)\*\*`)
	reBold2   = regexp.MustCompile(`__(.*?)__`)
	reItalic1 = regexp.MustCompile(`\*(.*?)\*`)
	reItalic2 = regexp.MustCompile(`_(.*?)_`)
)

type confluenceManager struct {
	sm       confluenceSecurity
	client   tools.HTTPClient
	provider *atlassianProvider
}

// NewConfluenceManager creates a new instance of confluenceManager.
func NewConfluenceManager(sm confluenceSecurity, client tools.HTTPClient) (*confluenceManager, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	provider, err := newAtlassianProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Atlassian provider: %w", err)
	}
	return &confluenceManager{
		sm:       sm,
		client:   client,
		provider: provider,
	}, nil
}

func (m *confluenceManager) resolveSpaceID(ctx context.Context, spaceKey string) (string, error) {
	// If it's already numeric, return it
	if _, err := strconv.Atoi(spaceKey); err == nil {
		return spaceKey, nil
	}

	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/wiki/api/v2/spaces")
	q := u.Query()
	q.Set("keys", spaceKey)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("space key '%s' not found", spaceKey)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to resolve space key '%s', status: %s", spaceKey, resp.Status)
	}

	var responseData struct {
		Results []struct {
			ID string `json:"id"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return "", err
	}

	if len(responseData.Results) == 0 {
		return "", fmt.Errorf("space key '%s' not found", spaceKey)
	}

	return responseData.Results[0].ID, nil
}

type searchResponse struct {
	Results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

type pageVersionInfo struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
}

func (m *confluenceManager) fetchSearchPage(ctx context.Context, url string) (*searchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("confluence API returned status %s; additionally, failed to read response body: %w", resp.Status, err)
		}
		return nil, fmt.Errorf("confluence API returned status: %s, body: %s", resp.Status, string(body))
	}

	var responseData searchResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &responseData, nil
}

type searchMatch struct {
	ID    string
	Title string
}

func (m *confluenceManager) processSearchResults(results []struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}, keyword string) []searchMatch {
	var matches []searchMatch
	keyword = strings.ToLower(keyword)
	for _, page := range results {
		if keyword == "" || strings.Contains(strings.ToLower(page.Title), keyword) {
			matches = append(matches, searchMatch{ID: page.ID, Title: page.Title})
		}
	}
	return matches
}

func (m *confluenceManager) resolveNextURL(baseURL, nextPath string) string {
	if nextPath == "" {
		return ""
	}
	if strings.HasPrefix(nextPath, "http") {
		return nextPath
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	parsedNext, err := url.Parse(nextPath)
	if err != nil {
		return ""
	}
	return parsedBase.ResolveReference(parsedNext).String()
}

func (m *confluenceManager) ConfluenceSearch(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Title   string `json:"title"`
		SpaceID string `json:"space_id"`
		Limit   int    `json:"limit"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Title != "" && params.SpaceID == "" {
		return tools.ToolResult{}, fmt.Errorf("space_id is required for fuzzy keyword search")
	}

	spaceID, err := m.resolveSpaceIDIfNeeded(ctx, params.SpaceID)
	if err != nil {
		return tools.ToolResult{}, err
	}

	u, err := m.prepareSearchURL(spaceID)
	if err != nil {
		return tools.ToolResult{}, err
	}

	maxPages, warning := m.getSearchLimit(params.Limit)
	nextURL := u.String()
	var matches []searchMatch
	maxIterations := (maxPages + 49) / 50
	pagesProcessed := 0

	for i := 0; i < maxIterations && nextURL != ""; i++ {
		responseData, err := m.fetchSearchPage(ctx, nextURL)
		if err != nil {
			return tools.ToolResult{}, err
		}

		pageMatches := m.processSearchResults(responseData.Results, params.Title)
		matches = append(matches, pageMatches...)
		pagesProcessed += len(responseData.Results)

		nextURL = m.resolveNextURL(m.provider.baseURL, responseData.Links.Next)
	}

	return m.formatSearchResults(matches, warning, pagesProcessed, params.SpaceID, params.Title), nil
}

func (m *confluenceManager) resolveSpaceIDIfNeeded(ctx context.Context, spaceID string) (string, error) {
	if spaceID == "" {
		return "", nil
	}
	resolved, err := m.resolveSpaceID(ctx, spaceID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve space ID for '%s': %w", spaceID, err)
	}
	return resolved, nil
}

func (m *confluenceManager) prepareSearchURL(spaceID string) (*url.URL, error) {
	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/wiki/api/v2/pages")

	q := u.Query()
	if spaceID != "" {
		q.Set("space-id", spaceID)
	}
	q.Set("limit", "50")
	q.Set("sort", "-modified-date")
	u.RawQuery = q.Encode()
	return u, nil
}

func (m *confluenceManager) getSearchLimit(limit int) (int, string) {
	maxPages := limit
	if maxPages <= 0 {
		maxPages = 1000
	}
	warning := ""
	if maxPages > 1000 {
		maxPages = 1000
		warning = "Note: Search depth capped at 1000 pages for safety.\n\n"
	}
	return maxPages, warning
}

func (m *confluenceManager) formatSearchResults(matches []searchMatch, warning string, pagesProcessed int, spaceID, title string) tools.ToolResult {
	if len(matches) == 0 {
		if title != "" {
			return tools.ToolResult{Text: fmt.Sprintf("%sSearched the %d most recently modified pages in space '%s' but found no pages containing '%s'. Please try an exact title or a different keyword.", warning, pagesProcessed, spaceID, title)}
		}
		return tools.ToolResult{Text: warning + "No pages found matching the criteria."}
	}

	var resultText strings.Builder
	resultText.WriteString(warning)
	resultText.WriteString("Found pages:\n")
	for _, page := range matches {
		_, _ = fmt.Fprintf(&resultText, "- %s (ID: %s)\n", page.Title, page.ID)
	}

	return tools.ToolResult{Text: resultText.String()}
}

func (m *confluenceManager) fetchPageContent(ctx context.Context, pageID string) (*http.Response, error) {
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
		_ = resp.Body.Close()
		return nil, fmt.Errorf("confluence page not found: %s", pageID)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("authentication failed: %s", resp.Status)
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("confluence API returned status: %s", resp.Status)
	}

	return resp, nil
}

func (m *confluenceManager) readAndValidateBody(respBody io.ReadCloser) ([]byte, error) {
	defer func() { _ = respBody.Close() }()
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

func (m *confluenceManager) decodePageResponse(body []byte) (title, storageValue string, err error) {
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

func (m *confluenceManager) ConfluenceRead(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

func (m *confluenceManager) getCurrentPageVersion(ctx context.Context, pageID string) (*pageVersionInfo, error) {
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch current version, status: %s", resp.Status)
	}

	var currentData pageVersionInfo
	if err := json.NewDecoder(resp.Body).Decode(&currentData); err != nil {
		return nil, fmt.Errorf("failed to decode current page data: %w", err)
	}
	return &currentData, nil
}

func (m *confluenceManager) executeUpdate(ctx context.Context, pageID string, payload map[string]interface{}) error {
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
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusConflict {
		return fmt.Errorf("conflict: page version changed during update; please retry")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("update failed with status: %s", resp.Status)
	}

	return nil
}

func (m *confluenceManager) ConfluenceWrite(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

type confluenceSecurity interface {
	security.ActionConfirmer
}
