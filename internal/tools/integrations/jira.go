// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type jiraManager struct {
	sm       *security.SecurityManager
	client   tools.HTTPClient
	provider *atlassianProvider
}

// NewJiraManager creates a new instance of jiraManager.
func NewJiraManager(sm *security.SecurityManager, client tools.HTTPClient) *jiraManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &jiraManager{
		sm:       sm,
		client:   client,
		provider: newAtlassianProvider(),
	}
}

func (m *jiraManager) jiraSearchIssues(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		JQL string `json:"jql"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.JQL == "" {
		return tools.ToolResult{}, fmt.Errorf("jql argument is required")
	}

	authHeader, err := m.provider.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	u, err := url.Parse(m.provider.baseURL)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid base url: %w", err)
	}
	u = u.JoinPath("/rest/api/3/search/jql")
	q := u.Query()
	q.Set("jql", params.JQL)
	q.Set("maxResults", "50")
	q.Set("fields", "summary,status")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return tools.ToolResult{}, fmt.Errorf("jira API returned status: %s, body: %s", resp.Status, string(body))
	}

	var responseData struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary string `json:"summary"`
				Status  struct {
					Name string `json:"name"`
				} `json:"status"`
			} `json:"fields"`
		} `json:"issues"`
		Total int `json:"total"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Issues) == 0 {
		return tools.ToolResult{Text: "No issues found matching the JQL query."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Found %d issues (showing up to 50):\n", responseData.Total))
	for _, issue := range responseData.Issues {
		resultText.WriteString(fmt.Sprintf("- [%s] %s (Status: %s)\n", issue.Key, issue.Fields.Summary, issue.Fields.Status.Name))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *jiraManager) jiraGetIssue(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		IssueKey string `json:"issue_key"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.IssueKey == "" {
		return tools.ToolResult{}, fmt.Errorf("issue_key argument is required")
	}

	authHeader, err := m.provider.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
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
	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return tools.ToolResult{}, fmt.Errorf("jira issue not found: %s", params.IssueKey)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return tools.ToolResult{}, fmt.Errorf("jira API returned status: %s, body: %s", resp.Status, string(body))
	}

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

	if err := json.NewDecoder(resp.Body).Decode(&issueData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	descriptionText := m.parseADF(issueData.Fields.Description)

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("# [%s] %s\n\n", issueData.Key, issueData.Fields.Summary))
	resultText.WriteString(fmt.Sprintf("- **Status**: %s\n", issueData.Fields.Status.Name))
	resultText.WriteString(fmt.Sprintf("- **Priority**: %s\n", issueData.Fields.Priority.Name))
	assignee := issueData.Fields.Assignee.DisplayName
	if assignee == "" {
		assignee = "Unassigned"
	}
	resultText.WriteString(fmt.Sprintf("- **Assignee**: %s\n\n", assignee))
	resultText.WriteString("## Description\n")
	if descriptionText == "" {
		resultText.WriteString("No description provided.")
	} else {
		resultText.WriteString(descriptionText)
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

// parseADF extracts plain text from Atlassian Document Format (ADF)
func (m *jiraManager) parseADF(adf interface{}) string {
	if adf == nil {
		return ""
	}

	var builder strings.Builder
	m.extractTextFromADF(adf, &builder)
	return strings.TrimSpace(builder.String())
}

func (m *jiraManager) extractTextFromADF(node interface{}, builder *strings.Builder) {
	switch v := node.(type) {
	case map[string]interface{}:
		if nodeType, ok := v["type"].(string); ok {
			if nodeType == "text" {
				if text, ok := v["text"].(string); ok {
					builder.WriteString(text)
				}
			}
			// Handle line breaks or paragraphs if needed for better readability
			if nodeType == "paragraph" || nodeType == "heading" {
				if builder.Len() > 0 && !strings.HasSuffix(builder.String(), "\n") {
					builder.WriteString("\n")
				}
			}
		}
		if content, ok := v["content"].([]interface{}); ok {
			for _, child := range content {
				m.extractTextFromADF(child, builder)
			}
			if nodeType, ok := v["type"].(string); ok && (nodeType == "paragraph" || nodeType == "heading") {
				builder.WriteString("\n")
			}
		}
	case []interface{}:
		for _, item := range v {
			m.extractTextFromADF(item, builder)
		}
	}
}
