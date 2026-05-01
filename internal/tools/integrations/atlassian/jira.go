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
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type JiraManager struct {
	sm       security.PathValidator
	client   tools.HTTPClient
	provider *AtlassianProvider
}

// NewJiraManager creates a new instance of JiraManager.
func NewJiraManager(sm security.PathValidator, client tools.HTTPClient) (*JiraManager, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	provider, err := NewAtlassianProvider()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Atlassian provider: %w", err)
	}
	return &JiraManager{
		sm:       sm,
		client:   client,
		provider: provider,
	}, nil
}

func (m *JiraManager) JiraSearchIssues(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		JQL   string `json:"jql"`
		Limit int    `json:"limit"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.JQL == "" {
		return tools.ToolResult{}, fmt.Errorf("jql argument is required")
	}

	u, err := m.prepareSearchURL(params.JQL, params.Limit)
	if err != nil {
		return tools.ToolResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("jira API returned status %s; additionally, failed to read response body: %w", resp.Status, err)
		}
		return tools.ToolResult{}, fmt.Errorf("jira API returned status: %s, body: %s", resp.Status, string(body))
	}

	resultText, err := m.formatSearchResponse(resp.Body)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: resultText}, nil
}

func (m *JiraManager) prepareSearchURL(jql string, limit int) (*url.URL, error) {
	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/rest/api/3/search/jql")
	q := u.Query()
	q.Set("jql", jql)

	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	q.Set("maxResults", strconv.Itoa(limit))
	q.Set("fields", "summary,status,assignee")
	u.RawQuery = q.Encode()
	return u, nil
}

func (m *JiraManager) formatSearchResponse(body io.ReadCloser) (string, error) {
	var responseData struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
				Assignee struct {
					DisplayName string `json:"displayName"`
				} `json:"assignee"`
			} `json:"fields"`
		} `json:"issues"`
		Total int `json:"total"`
	}

	if err := json.NewDecoder(body).Decode(&responseData); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Issues) == 0 {
		return "No issues found matching the JQL query.", nil
	}

	totalFound := responseData.Total
	if totalFound == 0 && len(responseData.Issues) > 0 {
		totalFound = len(responseData.Issues)
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Found %d issues (showing %d):\n", totalFound, len(responseData.Issues))
	for _, issue := range responseData.Issues {
		assignee := issue.Fields.Assignee.DisplayName
		if assignee == "" {
			assignee = "Unassigned"
		}
		_, _ = fmt.Fprintf(&resultText, "- [%s] %s (Status: %s, Assignee: %s)\n", issue.Key, issue.Fields.Summary, issue.Fields.Status.Name, assignee)
	}

	return resultText.String(), nil
}

func (m *JiraManager) JiraGetIssue(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		IssueKey string `json:"issue_key"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.IssueKey == "" {
		return tools.ToolResult{}, fmt.Errorf("issue_key argument is required")
	}

	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/rest/api/3/issue", params.IssueKey)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	req.Header.Set("Accept", "application/json")

	resp, err := m.provider.Do(ctx, m.client, req)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return tools.ToolResult{}, fmt.Errorf("jira issue not found: %s", params.IssueKey)
	}
	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("jira API returned status %s; additionally, failed to read response body: %w", resp.Status, err)
		}
		return tools.ToolResult{}, fmt.Errorf("jira API returned status: %s, body: %s", resp.Status, string(body))
	}

	resultText, err := m.formatGetIssueResponse(resp.Body)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: resultText}, nil
}

func (m *JiraManager) formatGetIssueResponse(body io.ReadCloser) (string, error) {
	var issueData struct {
		Key    string `json:"key"`
		Fields struct {
			Summary string `json:"summary"`
			Status  struct {
				Name string `json:"name"`
			} `json:"status"`
			Priority struct {
				Name string `json:"name"`
			} `json:"priority"`
			Assignee struct {
				DisplayName string `json:"displayName"`
			} `json:"assignee"`
			Description interface{} `json:"description"`
		} `json:"fields"`
	}

	if err := json.NewDecoder(body).Decode(&issueData); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	descriptionText := m.parseADF(issueData.Fields.Description)

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "# [%s] %s\n\n", issueData.Key, issueData.Fields.Summary)
	_, _ = fmt.Fprintf(&resultText, "- **Status**: %s\n", issueData.Fields.Status.Name)
	_, _ = fmt.Fprintf(&resultText, "- **Priority**: %s\n", issueData.Fields.Priority.Name)
	assignee := issueData.Fields.Assignee.DisplayName
	if assignee == "" {
		assignee = "Unassigned"
	}
	_, _ = fmt.Fprintf(&resultText, "- **Assignee**: %s\n\n", assignee)
	resultText.WriteString("## Description\n")
	if descriptionText == "" {
		resultText.WriteString("No description provided.")
	} else {
		resultText.WriteString(descriptionText)
	}

	return resultText.String(), nil
}

// parseADF extracts plain text from Atlassian Document Format (ADF)
func (m *JiraManager) parseADF(adf interface{}) string {
	if adf == nil {
		return ""
	}

	var builder strings.Builder
	m.extractTextFromADF(adf, &builder)
	return strings.TrimSpace(builder.String())
}

func (m *JiraManager) extractTextFromADF(node interface{}, builder *strings.Builder) {
	switch v := node.(type) {
	case map[string]interface{}:
		m.handleMapNode(v, builder)
	case []interface{}:
		m.handleContent(v, builder)
	}
}

func (m *JiraManager) handleMapNode(v map[string]interface{}, builder *strings.Builder) {
	nodeType, _ := v["type"].(string)

	if nodeType == "text" {
		if text, ok := v["text"].(string); ok {
			builder.WriteString(text)
		}
	}

	isBlock := nodeType == "paragraph" || nodeType == "heading"
	if isBlock {
		m.ensureNewline(builder)
	}

	if content, ok := v["content"].([]interface{}); ok {
		m.handleContent(content, builder)
		if isBlock {
			builder.WriteString("\n")
		}
	}
}

func (m *JiraManager) handleContent(content []interface{}, builder *strings.Builder) {
	for _, child := range content {
		m.extractTextFromADF(child, builder)
	}
}

func (m *JiraManager) ensureNewline(builder *strings.Builder) {
	if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
		builder.WriteString("\n")
	}
}
