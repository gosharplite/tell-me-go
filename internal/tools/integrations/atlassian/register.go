// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

import (
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// RegisterConfluence registers all Confluence tools into the provided registry.
func RegisterConfluence(r tools.Registry, sm security.Manager, client tools.HTTPClient) error {
	m, err := NewConfluenceManager(sm, client)
	if err != nil {
		// Skip registration gracefully if configuration is missing.
		return nil
	}

	if err := r.RegisterToToolkit("confluence", &tools.ToolDeclaration{
		Name:        "confluence_search",
		Description: "Performs a discovery-based search for Confluence pages using keywords. space_id is required for keyword searches. If title is omitted, it lists up to 250 recent pages in the space.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"title": {
					Type:        "STRING",
					Description: "Filter pages by title keywords (case-insensitive).",
				},
				"space_id": {
					Type:        "STRING",
					Description: "The space ID to search in. Required if title is provided.",
				},
				"limit": {
					Type:        "INTEGER",
					Description: "The maximum number of pages to search (default 1000, max 1000).",
				},
			},
		},
	}, m.confluenceSearch); err != nil {
		return err
	}

	if err := r.RegisterToToolkit("confluence", &tools.ToolDeclaration{
		Name:        "confluence_read",
		Description: "Reads the content of a Confluence page and converts it to clean Markdown. Requires a numeric page_id.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"page_id": {
					Type:        "STRING",
					Description: "The numeric ID of the page to fetch.",
				},
			},
			Required: []string{"page_id"},
		},
	}, m.ConfluenceRead); err != nil {
		return err
	}

	if err := r.RegisterToToolkitWithOptions("confluence", &tools.ToolDeclaration{
		Name:            "confluence_write",
		Description:     "Updates a Confluence page. Handles versioning and Markdown-to-XHTML conversion internally. Triggers security confirmation.",
		RequiresConsent: true,
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"page_id": {
					Type:        "STRING",
					Description: "The ID of the page to update.",
				},
				"markdown_content": {
					Type:        "STRING",
					Description: "The new body content in Markdown.",
				},
				"title": {
					Type:        "STRING",
					Description: "New title for the page (optional).",
				},
				"update_message": {
					Type:        "STRING",
					Description: "Summary for the version history (optional).",
				},
			},
			Required: []string{"page_id", "markdown_content"},
		},
	}, m.confluenceWrite, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}

// RegisterJira registers all Jira tools into the provided registry.
func RegisterJira(r tools.Registry, sm security.Manager, client tools.HTTPClient) error {
	m, err := NewJiraManager(sm, client)
	if err != nil {
		// Skip registration gracefully if configuration is missing.
		return nil
	}

	if err := r.RegisterToToolkit("jira", &tools.ToolDeclaration{
		Name:        "jira_search_issues",
		Description: "Searches for Jira issues using JQL (Jira Query Language). Returns issue keys, summaries, statuses, and assignees.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"jql": {
					Type:        "STRING",
					Description: "The JQL query string.",
				},
				"limit": {
					Type:        "INTEGER",
					Description: "Maximum number of issues to return (default 100, max 1000).",
				},
			},
			Required: []string{"jql"},
		},
	}, m.JiraSearchIssues); err != nil {
		return err
	}

	if err := r.RegisterToToolkit("jira", &tools.ToolDeclaration{
		Name:        "jira_get_issue",
		Description: "Retrieves full details for a specific Jira issue, including summary, status, priority, assignee, and description.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"issue_key": {
					Type:        "STRING",
					Description: "The Jira issue key (e.g., 'PROJ-123').",
				},
			},
			Required: []string{"issue_key"},
		},
	}, m.JiraGetIssue); err != nil {
		return err
	}
	return nil
}
