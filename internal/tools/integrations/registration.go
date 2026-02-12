// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// RegisterAll registers all external integration tools.
func RegisterAll(r *registry.Registry, sm *security.SecurityManager, client llm.LLMClient, assetsDir string) {
	// Register Media Tools
	registerMedia(r, sm, client, assetsDir)

	// Register Network Tools
	net := newnetworkTool(sm, nil)
	registerNetwork(r, net)

	// Register Teams Tools
	registerTeams(r, sm, nil)

	// Register Confluence Tools
	registerConfluence(r, sm, nil)

	// Register Jira Tools
	registerJira(r, sm, nil)

	// Register Azure DevOps Tools
	registerAzureDevOps(r, sm, nil)
}

func registerMedia(r *registry.Registry, sm *security.SecurityManager, client llm.LLMClient, assetsDir string) {
	m := &mediaManager{sm: sm, client: client, assetsDir: assetsDir}

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "create_image",
		Description: "Generates an image from a text prompt using an Imagen model (default: imagen-3.0-generate-001). Saves to assets/generated/.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"prompt": {
					Type:        "STRING",
					Description: "Detailed description of the image to generate.",
				},
				"aspect_ratio": {
					Type:        "STRING",
					Description: "Aspect ratio (e.g., '1:1', '4:3', '16:9'). Default '1:1'.",
				},
				"model": {
					Type:        "STRING",
					Description: "The model to use for generation (e.g., 'imagen-3.0-generate-001', 'imagen-3.0-fast-001').",
				},
			},
			Required: []string{"prompt"},
		},
	}, m.createImage, registry.ToolOptions{LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "read_image",
		Description: "Reads a local image file for vision analysis.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the image file (e.g., './assets/screenshot.png').",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readImage)
}

func registerNetwork(r *registry.Registry, net *networkTool) {
	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches and cleans content from a URL, stripping HTML tags and scripts to provide readable documentation. Useful for researching library APIs.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"url": {
					Type:        "STRING",
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, net.ReadExternalDocs, registry.ToolOptions{LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"method": {
					Type:        "STRING",
					Description: "HTTP method (GET, POST, PUT, DELETE, etc.).",
				},
				"url": {
					Type:        "STRING",
					Description: "The target URL.",
				},
				"headers": {
					Type:        "OBJECT",
					Description: "HTTP headers as a map of strings.",
					Properties: map[string]*tools.Schema{
						"Content-Type": {Type: "STRING"},
					},
				},
				"body": {
					Type:        "STRING",
					Description: "Request body content.",
				},
			},
			Required: []string{"method", "url"},
		},
	}, net.HttpRequest, registry.ToolOptions{LongRunning: true})
}

func registerTeams(r *registry.Registry, sm *security.SecurityManager, client tools.HTTPClient) {
	m := newteamsManager(sm, client)
	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "send_teams_message",
		Description: "Sends a message to a Microsoft Teams channel using a Power Automate workflow webhook.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"webhook_url": {
					Type:        "STRING",
					Description: "The Power Automate workflow URL (must start with https://).",
				},
				"message": {
					Type:        "STRING",
					Description: "The message to send to the channel.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for sending this message.",
				},
			},
			Required: []string{"webhook_url", "message", "reason"},
		},
	}, m.sendTeamsMessage, registry.ToolOptions{Serial: true})
}

func registerConfluence(r *registry.Registry, sm *security.SecurityManager, client tools.HTTPClient) {
	m := newconfluenceManager(sm, client)

	r.Register(&tools.ToolDeclaration{
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
	}, m.confluenceSearch)

	r.Register(&tools.ToolDeclaration{
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
	}, m.confluenceRead)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "confluence_write",
		Description: "Updates a Confluence page. Handles versioning and Markdown-to-XHTML conversion internally. Triggers security confirmation.",
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
	}, m.confluenceWrite, registry.ToolOptions{Serial: true})
}

func registerJira(r *registry.Registry, sm *security.SecurityManager, client tools.HTTPClient) {
	m := newjiraManager(sm, client)

	r.Register(&tools.ToolDeclaration{
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
	}, m.jiraSearchIssues)

	r.Register(&tools.ToolDeclaration{
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
	}, m.jiraGetIssue)
}

func registerAzureDevOps(r *registry.Registry, sm *security.SecurityManager, client tools.HTTPClient) {
	m := newazureDevOpsManager(sm, client)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pull_request",
		Description: "Retrieves full metadata for a specific Azure DevOps Pull Request, including status, reviewers, and branch details.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"pull_request_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the pull request.",
				},
			},
			Required: []string{"organization", "project", "repository", "pull_request_id"},
		},
	}, m.adoGetPullRequest)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_list_pull_requests",
		Description: "Lists pull requests in a specific Azure DevOps repository, with optional status filtering.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"status": {
					Type:        "STRING",
					Description: "Filter by status (active, completed, abandoned, all). Default is active.",
				},
				"top": {
					Type:        "INTEGER",
					Description: "Maximum number of PRs to return (default 50).",
				},
			},
			Required: []string{"organization", "project", "repository"},
		},
	}, m.adoListPullRequests)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pr_diff",
		Description: "Retrieves the file changes (diffs) for a specific Azure DevOps Pull Request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"pull_request_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the pull request.",
				},
			},
			Required: []string{"organization", "project", "repository", "pull_request_id"},
		},
	}, m.adoGetPrDiff)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pr_threads",
		Description: "Retrieves discussion threads and comments for a specific Azure DevOps Pull Request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"pull_request_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the pull request.",
				},
			},
			Required: []string{"organization", "project", "repository", "pull_request_id"},
		},
	}, m.adoGetPrThreads)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_file_content",
		Description: "Retrieves the content of a specific file from an Azure DevOps repository at a given path and version (branch/commit).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"path": {
					Type:        "STRING",
					Description: "The full path to the file in the repository (e.g., '/src/main.go').",
				},
				"version": {
					Type:        "STRING",
					Description: "The branch name, commit hash, or tag. Default is main.",
				},
			},
			Required: []string{"organization", "project", "repository", "path"},
		},
	}, m.adoGetFileContent)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_list_repository_items",
		Description: "Lists items (files and folders) in a specific directory of an Azure DevOps repository.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"scope_path": {
					Type:        "STRING",
					Description: "The directory path to list. Default is / (root).",
				},
				"version": {
					Type:        "STRING",
					Description: "The branch name, commit hash, or tag. Default is main.",
				},
				"recursion_level": {
					Type:        "STRING",
					Description: "Recursion level: none (default), oneLevel, or full.",
				},
			},
			Required: []string{"organization", "project", "repository"},
		},
	}, m.adoListRepositoryItems)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_list_pipeline_runs",
		Description: "Lists recent runs (executions) for a specific Azure DevOps pipeline.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"pipeline_id": {
					Type:        "INTEGER",
					Description: "The ID of the pipeline definition.",
				},
				"top": {
					Type:        "INTEGER",
					Description: "Maximum number of runs to return (default 10).",
				},
			},
			Required: []string{"organization", "project", "pipeline_id"},
		},
	}, m.adoListPipelineRuns)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pipeline_run",
		Description: "Retrieves the detailed status and result of a specific Azure DevOps pipeline run.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"pipeline_id": {
					Type:        "INTEGER",
					Description: "The ID of the pipeline definition.",
				},
				"run_id": {
					Type:        "INTEGER",
					Description: "The ID of the specific execution.",
				},
			},
			Required: []string{"organization", "project", "pipeline_id", "run_id"},
		},
	}, m.adoGetPipelineRun)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pipeline_logs",
		Description: "Fetches the console output/logs of a failed pipeline run for diagnosis.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"pipeline_id": {
					Type:        "INTEGER",
					Description: "The ID of the pipeline definition.",
				},
				"run_id": {
					Type:        "INTEGER",
					Description: "The ID of the specific execution.",
				},
				"log_id": {
					Type:        "INTEGER",
					Description: "The specific log ID. If omitted, the tool lists available logs.",
				},
			},
			Required: []string{"organization", "project", "pipeline_id", "run_id"},
		},
	}, m.adoGetPipelineLogs)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pr_statuses",
		Description: "Retrieves the real-time status of all automated checks (builds, tests, quality gates) for a specific Azure DevOps Pull Request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"pull_request_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the pull request.",
				},
			},
			Required: []string{"organization", "project", "repository", "pull_request_id"},
		},
	}, m.adoGetPrStatuses)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_pr_policy_evaluations",
		Description: "Retrieves the real-time status of all automated policy evaluations (build gates, reviewer requirements) for a specific Azure DevOps Pull Request.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"pull_request_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the pull request.",
				},
			},
			Required: []string{"organization", "project", "repository", "pull_request_id"},
		},
	}, m.adoGetPrPolicyEvaluations)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_list_branch_policies",
		Description: "Lists all branch policies (build gates, reviewer requirements, etc.) configured for a specific branch in an Azure DevOps repository.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"repository": {
					Type:        "STRING",
					Description: "The repository name or ID.",
				},
				"branch_name": {
					Type:        "STRING",
					Description: "The branch name (e.g., 'main', 'dev/1.54').",
				},
			},
			Required: []string{"organization", "project", "repository", "branch_name"},
		},
	}, m.adoListBranchPolicies)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_build_timeline",
		Description: "Retrieves the build timeline, providing the state, result, and log metadata for every task in the build.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"build_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the build (not the pipeline ID).",
				},
			},
			Required: []string{"organization", "project", "build_id"},
		},
	}, m.adoGetBuildTimeline)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_task_log",
		Description: "Retrieves the raw console output/logs for a specific build task.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"build_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the build.",
				},
				"log_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the specific log record (retrieved from the build timeline).",
				},
			},
			Required: []string{"organization", "project", "build_id", "log_id"},
		},
	}, m.adoGetTaskLog)

	r.Register(&tools.ToolDeclaration{
		Name:        "ado_get_build_changes",
		Description: "Retrieves the list of commits/changes included in a specific build.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"organization": {
					Type:        "STRING",
					Description: "The Azure DevOps organization name.",
				},
				"project": {
					Type:        "STRING",
					Description: "The project name or ID.",
				},
				"build_id": {
					Type:        "INTEGER",
					Description: "The numeric ID of the build.",
				},
				"top": {
					Type:        "INTEGER",
					Description: "Maximum number of changes to return (default 50).",
				},
			},
			Required: []string{"organization", "project", "build_id"},
		},
	}, m.adoGetBuildChanges)
}
