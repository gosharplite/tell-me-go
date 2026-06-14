// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type searchResponse struct {
	Results []struct {
		ID    string `json:"id"`
		Title string `json:"title"`
	} `json:"results"`
	Links struct {
		Next string `json:"next"`
	} `json:"_links"`
}

type searchMatch struct {
	ID    string
	Title string
}

func (m *ConfluenceManager) resolveSpaceID(ctx context.Context, spaceKey string) (string, error) {
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
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body) // drain to enable connection reuse
		_ = resp.Body.Close()
	}()

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

func (m *ConfluenceManager) FetchSearchPage(ctx context.Context, url string) (*searchResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

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

func (m *ConfluenceManager) processSearchResults(results []struct {
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

func (m *ConfluenceManager) resolveNextURL(baseURL, nextPath string) string {
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

func (m *ConfluenceManager) confluenceSearch(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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
		responseData, err := m.FetchSearchPage(ctx, nextURL)
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

func (m *ConfluenceManager) resolveSpaceIDIfNeeded(ctx context.Context, spaceID string) (string, error) {
	if spaceID == "" {
		return "", nil
	}
	resolved, err := m.resolveSpaceID(ctx, spaceID)
	if err != nil {
		return "", fmt.Errorf("failed to resolve space ID for '%s': %w", spaceID, err)
	}
	return resolved, nil
}

func (m *ConfluenceManager) prepareSearchURL(spaceID string) (*url.URL, error) {
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

func (m *ConfluenceManager) getSearchLimit(limit int) (int, string) {
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

func (m *ConfluenceManager) formatSearchResults(matches []searchMatch, warning string, pagesProcessed int, spaceID, title string) tools.ToolResult {
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
