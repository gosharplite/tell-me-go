// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"os"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// RegisterAll registers all external integration tools.
func RegisterAll(r tools.Registry, sm domain_security.ISecurityManager, client llm.LLMClient, assetsDir string) error {
	// Register Media Tools
	if err := registerMedia(r, sm, client, assetsDir); err != nil {
		return err
	}

	// Register Network Tools
	net := newnetworkTool(sm, nil)
	if err := registerNetwork(r, net); err != nil {
		return err
	}

	// Register Teams Tools
	if err := registerTeams(r, sm, nil); err != nil {
		return err
	}

	// Register Confluence Tools
	if err := registerConfluence(r, sm, nil); err != nil {
		return err
	}

	// Register Jira Tools
	if err := registerJira(r, sm, nil); err != nil {
		return err
	}

	// Register Azure DevOps Tools
	if err := registerAzureDevOps(r, sm, nil); err != nil {
		return err
	}

	return nil
}

func registerMedia(r tools.Registry, sm domain_security.ISecurityManager, client llm.LLMClient, assetsDir string) error {
	m := &mediaManager{sm: sm, client: client, assetsDir: assetsDir}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, m.createImage, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
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
	}, m.readImage); err != nil {
		return err
	}
	return nil
}

func registerNetwork(r tools.Registry, net *networkTool) error {
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, net.ReadExternalDocs, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, net.HttpRequest, tools.ToolOptions{LongRunning: true}); err != nil {
		return err
	}
	return nil
}

func registerTeams(r tools.Registry, sm domain_security.ISecurityManager, client tools.HTTPClient) error {
	m := newteamsManager(sm, client)
	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:            "send_teams_message",
		Description:     "Sends a message to a Microsoft Teams channel using a Power Automate workflow webhook.",
		RequiresConsent: true,
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
	}, m.sendTeamsMessage, tools.ToolOptions{Serial: true}); err != nil {
		return err
	}
	return nil
}

func registerConfluence(r tools.Registry, sm domain_security.ISecurityManager, client tools.HTTPClient) error {
	m := newconfluenceManager(sm, client)

	if err := r.Register(&tools.ToolDeclaration{
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

	if err := r.Register(&tools.ToolDeclaration{
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
	}, m.confluenceRead); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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

func registerJira(r tools.Registry, sm domain_security.ISecurityManager, client tools.HTTPClient) error {
	m := newjiraManager(sm, client)

	if err := r.Register(&tools.ToolDeclaration{
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
	}, m.jiraSearchIssues); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
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
	}, m.jiraGetIssue); err != nil {
		return err
	}
	return nil
}

func registerAzureDevOps(r tools.Registry, sm domain_security.ISecurityManager, client tools.HTTPClient) error {
	var opts []adoOption
	if client != nil {
		opts = append(opts, withHTTPClient(client))
	}
	if token := os.Getenv("AZURE_PAT_ALL"); token != "" {
		opts = append(opts, withToken(token))
	}
	m := newADOManager(sm, opts...)

	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pull_request",
				Description: "Retrieves full metadata for a specific Azure DevOps Pull Request, including status, reviewers, and branch details.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"pull_request_id": {Type: "INTEGER", Description: "The numeric ID of the pull request."},
					},
					Required: []string{"organization", "project", "repository", "pull_request_id"},
				},
			},
			handler: m.adoGetPullRequest,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_pull_requests",
				Description: "Lists pull requests in a specific Azure DevOps repository, with optional status filtering.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"repository":   {Type: "STRING", Description: "The repository name or ID."},
						"status":       {Type: "STRING", Description: "Filter by status (active, completed, abandoned, all). Default is active."},
						"top":          {Type: "INTEGER", Description: "Maximum number of PRs to return (default 50)."},
					},
					Required: []string{"organization", "project", "repository"},
				},
			},
			handler: m.adoListPullRequests,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pr_diff",
				Description: "Retrieves the file changes (diffs) for a specific Azure DevOps Pull Request.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"pull_request_id": {Type: "INTEGER", Description: "The numeric ID of the pull request."},
					},
					Required: []string{"organization", "project", "repository", "pull_request_id"},
				},
			},
			handler: m.adoGetPrDiff,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pr_threads",
				Description: "Retrieves discussion threads and comments for a specific Azure DevOps Pull Request.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"pull_request_id": {Type: "INTEGER", Description: "The numeric ID of the pull request."},
					},
					Required: []string{"organization", "project", "repository", "pull_request_id"},
				},
			},
			handler: m.adoGetPrThreads,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_file_content",
				Description: "Retrieves the content of a specific file from an Azure DevOps repository at a given path and version (branch/commit).",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"repository":   {Type: "STRING", Description: "The repository name or ID."},
						"path":         {Type: "STRING", Description: "The full path to the file in the repository (e.g., '/src/main.go')."},
						"version":      {Type: "STRING", Description: "The branch name, commit hash, or tag. Default is main."},
					},
					Required: []string{"organization", "project", "repository", "path"},
				},
			},
			handler: m.adoGetFileContent,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_repository_items",
				Description: "Lists items (files and folders) in a specific directory of an Azure DevOps repository.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"scope_path":      {Type: "STRING", Description: "The directory path to list. Default is / (root)."},
						"version":         {Type: "STRING", Description: "The branch name, commit hash, or tag. Default is main."},
						"recursion_level": {Type: "STRING", Description: "Recursion level: none (default), oneLevel, or full."},
					},
					Required: []string{"organization", "project", "repository"},
				},
			},
			handler: m.adoListRepositoryItems,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_pipelines",
				Description: "Lists pipeline names and their corresponding IDs in an Azure DevOps project.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
					},
					Required: []string{"organization", "project"},
				},
			},
			handler: m.adoListPipelines,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_pipeline_runs",
				Description: "Lists recent runs (executions) for a specific Azure DevOps pipeline.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"pipeline_id":   {Type: "INTEGER", Description: "The ID of the pipeline definition."},
						"pipeline_name": {Type: "STRING", Description: "The name of the pipeline (used if pipeline_id is omitted)."},
						"repository":    {Type: "STRING", Description: "Filter runs by repository ID (UUID) or exact internal reference name, as raw names are often omitted in run resource metadata."},
						"top":           {Type: "INTEGER", Description: "Maximum number of runs to return (default 10)."},
					},
					Required: []string{"organization", "project"},
				},
			},
			handler: m.adoListPipelineRuns,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pipeline_run",
				Description: "Retrieves the detailed status and result of a specific Azure DevOps pipeline run.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"pipeline_id":  {Type: "INTEGER", Description: "The ID of the pipeline definition."},
						"run_id":       {Type: "INTEGER", Description: "The ID of the specific execution."},
					},
					Required: []string{"organization", "project", "pipeline_id", "run_id"},
				},
			},
			handler: m.adoGetPipelineRun,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pipeline_definition",
				Description: "Retrieves the full configuration metadata of an existing Azure DevOps pipeline, including variables and security settings.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"pipeline_id":  {Type: "INTEGER", Description: "The numeric ID of the pipeline to inspect."},
					},
					Required: []string{"organization", "project", "pipeline_id"},
				},
			},
			handler: m.adoGetPipelineDefinition,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pipeline_logs",
				Description: "Fetches the console output/logs of a failed pipeline run for diagnosis.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"pipeline_id":   {Type: "INTEGER", Description: "The ID of the pipeline definition."},
						"run_id":        {Type: "INTEGER", Description: "The ID of the specific execution."},
						"log_id":        {Type: "INTEGER", Description: "The specific log ID. If omitted, the tool lists available logs."},
						"tail_lines":    {Type: "INTEGER", Description: "Return the last N lines. Default: 200"},
						"head_lines":    {Type: "INTEGER", Description: "Return the first N lines (optional)."},
						"filter_query":  {Type: "STRING", Description: "Regex to filter log lines (e.g. 'error|failed'). WARNING: This parameter OVERRIDES tail/head/pagination. It scans the ENTIRE file. Do NOT use greedy wildcards like '.*' as it will return massive payloads and crash the context."},
						"context_lines": {Type: "INTEGER", Description: "Lines of context around filter matches. Default: 5"},
						"start_line":    {Type: "INTEGER", Description: "Start line for pagination."},
						"max_lines":     {Type: "INTEGER", Description: "Maximum lines for pagination."},
					},
					Required: []string{"organization", "project", "pipeline_id", "run_id"},
				},
			},
			handler: m.adoGetPipelineLogs,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pr_statuses",
				Description: "Retrieves the real-time status of all automated checks (builds, tests, quality gates) for a specific Azure DevOps Pull Request.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"pull_request_id": {Type: "INTEGER", Description: "The numeric ID of the pull request."},
					},
					Required: []string{"organization", "project", "repository", "pull_request_id"},
				},
			},
			handler: m.adoGetPrStatuses,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_pr_policy_evaluations",
				Description: "Retrieves the real-time status of all automated policy evaluations (build gates, reviewer requirements) for a specific Azure DevOps Pull Request.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"repository":      {Type: "STRING", Description: "The repository name or ID."},
						"pull_request_id": {Type: "INTEGER", Description: "The numeric ID of the pull request."},
					},
					Required: []string{"organization", "project", "repository", "pull_request_id"},
				},
			},
			handler: m.adoGetPrPolicyEvaluations,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_branch_policies",
				Description: "Lists all branch policies (build gates, reviewer requirements, etc.) configured for a specific branch in an Azure DevOps repository.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"repository":   {Type: "STRING", Description: "The repository name or ID."},
						"branch_name":  {Type: "STRING", Description: "The branch name (e.g., 'main', 'dev/1.54')."},
					},
					Required: []string{"organization", "project", "repository", "branch_name"},
				},
			},
			handler: m.adoListBranchPolicies,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_build_timeline",
				Description: "Retrieves the build timeline, providing the state, result, and log metadata for every task in the build.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"build_id":     {Type: "INTEGER", Description: "The numeric ID of the build (not the pipeline ID)."},
					},
					Required: []string{"organization", "project", "build_id"},
				},
			},
			handler: m.adoGetBuildTimeline,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_task_log",
				Description: "Retrieves the raw console output/logs for a specific build task.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"build_id":      {Type: "INTEGER", Description: "The numeric ID of the build."},
						"log_id":        {Type: "INTEGER", Description: "The numeric ID of the specific log record (retrieved from the build timeline)."},
						"tail_lines":    {Type: "INTEGER", Description: "Return the last N lines. Default: 200"},
						"head_lines":    {Type: "INTEGER", Description: "Return the first N lines (optional)."},
						"filter_query":  {Type: "STRING", Description: "Regex to filter log lines (e.g. 'error|failed'). WARNING: This parameter OVERRIDES tail/head/pagination. It scans the ENTIRE file. Do NOT use greedy wildcards like '.*' as it will return massive payloads and crash the context."},
						"context_lines": {Type: "INTEGER", Description: "Lines of context around filter matches. Default: 5"},
						"start_line":    {Type: "INTEGER", Description: "Start line for pagination."},
						"max_lines":     {Type: "INTEGER", Description: "Maximum lines for pagination."},
					},
					Required: []string{"organization", "project", "build_id", "log_id"},
				},
			},
			handler: m.adoGetTaskLog,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_get_build_changes",
				Description: "Retrieves the list of commits/changes included in a specific build.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"build_id":     {Type: "INTEGER", Description: "The numeric ID of the build."},
						"top":          {Type: "INTEGER", Description: "Maximum number of changes to return (default 50)."},
					},
					Required: []string{"organization", "project", "build_id"},
				},
			},
			handler: m.adoGetBuildChanges,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_update_build_definition_variables",
				Description: "Modifies variables for an existing pipeline (build) definition using a Read-Modify-Write cycle. Useful for locking down variable overrides.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"definition_id": {Type: "INTEGER", Description: "The numeric ID of the build definition to update."},
						"variables":     {Type: "OBJECT", Description: "A map of variable names to objects with 'value', 'isSecret', and 'allowOverride' (CRITICAL for lockdown)."},
					},
					Required: []string{"organization", "project", "definition_id", "variables"},
				},
			},
			handler: m.adoUpdateBuildDefinitionVariables,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_create_pipeline",
				Description: "Creates a new Azure DevOps YAML build pipeline. Idempotent: returns existing ID if the name already exists. Triggers security confirmation.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":    {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":         {Type: "STRING", Description: "The project name or ID."},
						"name":            {Type: "STRING", Description: "The desired name of the new pipeline."},
						"repository_id":   {Type: "STRING", Description: "The UUID of the Git repository containing the YAML."},
						"yaml_path":       {Type: "STRING", Description: "The exact path to the YAML file in the repository (e.g., '/dev-3/caddy/cicd/main.yaml')."},
						"variable_groups": {Type: "ARRAY", Description: "A list of numeric IDs for existing Variable Groups (e.g., 'WebSC-Common-Vars') to be linked to the new pipeline.", Items: &tools.Schema{Type: "INTEGER"}},
						"variables":       {Type: "OBJECT", Description: "A collection of key-value pairs (name -> {value, isSecret, allowOverride}) to be stored directly in the pipeline's 'Variables' configuration. 'allowOverride' defaults to true, letting users change the value during a run."},
					},
					Required: []string{"organization", "project", "name", "repository_id", "yaml_path"},
				},
			},
			handler: m.adoCreatePipeline,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_run_pipeline",
				Description: "Triggers a manual run of an existing Azure DevOps YAML pipeline. Returns the Run ID for tracking. Triggers security confirmation.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":        {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":             {Type: "STRING", Description: "The project name or ID."},
						"pipeline_id":         {Type: "INTEGER", Description: "The numeric ID of the pipeline to run."},
						"branch":              {Type: "STRING", Description: "The branch or tag to build from (e.g., 'main', 'v1.2.0'). Full refs like 'refs/tags/v1.0' are also supported."},
						"template_parameters": {Type: "OBJECT", Description: "Key-value string pairs for YAML parameters."},
						"variables":           {Type: "OBJECT", Description: "Key-value string pairs for pipeline variables."},
					},
					Required: []string{"organization", "project", "pipeline_id"},
				},
			},
			handler: m.adoRunPipeline,
		},
	}

	for _, spec := range specs {
		if err := r.Register(spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
