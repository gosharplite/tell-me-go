// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// Register registers all Azure DevOps tools into the provided registry.
func Register(r tools.Registry, sm security.Manager, client tools.HTTPClient) error {
	var opts []AdoOption
	if client != nil {
		opts = append(opts, WithHTTPClient(client))
	}
	if token := os.Getenv("AZURE_PAT_ALL"); token != "" {
		opts = append(opts, WithToken(token))
	}
	m := NewADOManager(sm, opts...)
	adoFormatter := NewPipelineFormatter()

	marshalIndentResult := func(v interface{}) (tools.ToolResult, error) {
		output, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("marshaling response: %w", err)
		}
		return tools.ToolResult{Text: string(output)}, nil
	}

	appendTruncationNote := func(c LogContent) string {
		if !c.Truncated {
			return c.Content
		}
		return c.Content + fmt.Sprintf("\n\n[NOTE to LLM: Log content was truncated after %d lines for safety. Suggest using a more specific filter_query or pagination if the relevant data is missing.]", c.TotalLines)
	}

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
			handler: m.AdoGetPullRequest,
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
			handler: m.AdoListPullRequests,
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
			handler: m.AdoGetPrDiff,
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
			handler: m.AdoGetPrThreads,
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
			handler: m.AdoGetFileContent,
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
			handler: m.AdoListRepositoryItems,
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				pipelines, err := m.ListPipelines(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: adoFormatter.FormatPipelineList(pipelines)}, nil
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				pipelineID, runs, err := m.ListPipelineRuns(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: adoFormatter.FormatPipelineRunsList(pipelineID, runs)}, nil
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				run, err := m.GetPipelineRun(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: adoFormatter.FormatPipelineRunDetail(run)}, nil
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				def, err := m.GetPipelineDefinition(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return marshalIndentResult(def)
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				// Dispatch on whether log_id was supplied: list-mode vs fetch-mode.
				var dispatch struct {
					LogId int `json:"log_id"`
				}
				_ = tools.UnmarshalArgs(args, &dispatch)

				if dispatch.LogId == 0 {
					runID, logs, err := m.ListPipelineLogs(ctx, args)
					if err != nil {
						return tools.ToolResult{}, err
					}
					return tools.ToolResult{Text: adoFormatter.FormatPipelineLogList(runID, logs)}, nil
				}

				content, err := m.GetPipelineLogContent(ctx, args, hb)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: appendTruncationNote(content)}, nil
			},
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
			handler: m.AdoGetPrStatuses,
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
			handler: m.AdoGetPrPolicyEvaluations,
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
			handler: m.AdoListBranchPolicies,
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				records, err := m.GetBuildTimeline(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return marshalIndentResult(records)
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				content, err := m.GetTaskLog(ctx, args, hb)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return tools.ToolResult{Text: appendTruncationNote(content)}, nil
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				changes, err := m.GetBuildChanges(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				return marshalIndentResult(changes)
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				result, err := m.UpdateBuildDefinitionVariables(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				if result.Cancelled {
					return tools.ToolResult{Text: "Update cancelled by user."}, nil
				}
				return tools.ToolResult{Text: fmt.Sprintf("Successfully updated variables for build definition %d", result.DefinitionID)}, nil
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				result, err := m.CreatePipeline(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				switch {
				case result.AlreadyExisted:
					return tools.ToolResult{Text: fmt.Sprintf("Pipeline '%s' already exists with ID: %d", result.Name, result.PipelineID)}, nil
				case result.Cancelled:
					return tools.ToolResult{Text: "Pipeline creation cancelled by user."}, nil
				default:
					return tools.ToolResult{Text: fmt.Sprintf("Successfully created pipeline '%s' with ID: %d", result.Name, result.PipelineID)}, nil
				}
			},
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
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				// Pre-format the branch into a fully qualified ADO ref. Presentation
				// policy (vX.Y.Z -> refs/tags/, else refs/heads/) lives at the consumer.
				// The raw branch name stays under args["branch"] so the confirmation
				// prompt shown by RunPipeline displays the user-facing name; the formatted
				// ref is injected under args["_ref_name"] for the ADO API request.
				branchRaw, _ := args["branch"].(string)
				refName := adoFormatter.FormatBranchRef(branchRaw)

				// Shallow-copy args to avoid mutating the caller's map.
				argsCopy := make(map[string]interface{}, len(args)+1)
				for k, v := range args {
					argsCopy[k] = v
				}
				argsCopy["_ref_name"] = refName

				result, err := m.RunPipeline(ctx, argsCopy)
				if err != nil {
					return tools.ToolResult{}, err
				}
				if result.Cancelled {
					return tools.ToolResult{Text: "Pipeline run cancelled by user."}, nil
				}
				return tools.ToolResult{
					Text: fmt.Sprintf("Successfully triggered pipeline run ID: %d\nWeb URL: %s", result.RunID, result.WebURL),
				}, nil
			},
		},
	}

	for _, spec := range specs {
		if err := r.RegisterToToolkit("ado", spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
