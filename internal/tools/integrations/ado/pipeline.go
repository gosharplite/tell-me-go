// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type adoListPipelineRunsParams struct {
	Organization string `json:"organization"`
	Project      string `json:"project"`
	PipelineId   int    `json:"pipeline_id"`
	PipelineName string `json:"pipeline_name"`
	Repository   string `json:"repository"`
	Top          int    `json:"top"`

	// Computed fields
	OriginalTop int
	FetchTop    int
}

func parseListPipelineRunsArgs(args map[string]interface{}) (adoListPipelineRunsParams, error) {
	var params adoListPipelineRunsParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return params, fmt.Errorf("parsing list pipeline runs args: %w", err)
	}

	if params.Organization == "" || params.Project == "" {
		return params, fmt.Errorf("organization and project are required")
	}

	if params.PipelineId == 0 && params.PipelineName == "" {
		return params, fmt.Errorf("either pipeline_id or pipeline_name must be provided")
	}

	params.OriginalTop = params.Top
	if params.OriginalTop <= 0 {
		params.OriginalTop = 10
	} else if params.OriginalTop > 1000 {
		params.OriginalTop = 1000 // Defensive upper bound
	}

	params.FetchTop = params.OriginalTop
	if params.Repository != "" {
		params.FetchTop = 100 // Increased limit for manual filtering
	}

	return params, nil
}

type adoCreatePipelineParams struct {
	Organization   string                 `json:"organization"`
	Project        string                 `json:"project"`
	Name           string                 `json:"name"`
	RepositoryId   string                 `json:"repository_id"`
	YamlPath       string                 `json:"yaml_path"`
	VariableGroups []int                  `json:"variable_groups"`
	Variables      map[string]adoVariable `json:"variables"`
}

func parseCreatePipelineArgs(args map[string]interface{}) (adoCreatePipelineParams, error) {
	var params adoCreatePipelineParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return params, fmt.Errorf("parsing create pipeline args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Name == "" || params.RepositoryId == "" || params.YamlPath == "" {
		return params, fmt.Errorf("organization, project, name, repository_id, and yaml_path are required")
	}

	return params, nil
}

type adoRunPipelineParams struct {
	Organization       string            `json:"organization"`
	Project            string            `json:"project"`
	PipelineId         int               `json:"pipeline_id"`
	Branch             string            `json:"branch"`
	TemplateParameters map[string]string `json:"template_parameters"`
	Variables          map[string]string `json:"variables"`

	// RefName is supplied by the caller via "_ref_name"; MappedVariables is
	// computed from Variables.
	RefName         string `json:"_ref_name"`
	MappedVariables map[string]adoVariable
}

type adoVariable struct {
	Value                 string `json:"value"`
	IsSecret              bool   `json:"isSecret"`
	AllowOverride         *bool  `json:"allowOverride,omitempty"`
	IsSettableAtQueueTime *bool  `json:"isSettableAtQueueTime,omitempty"`
}

type adoVariableGroup struct {
	ID int `json:"id"`
}

type adoCreatePipelineRequest struct {
	Name          string                   `json:"name"`
	Configuration adoPipelineConfiguration `json:"configuration"`
}

type adoPipelineConfiguration struct {
	Type           string                 `json:"type"`
	Path           string                 `json:"path"`
	Repository     adoPipelineRepository  `json:"repository"`
	Variables      map[string]adoVariable `json:"variables,omitempty"`
	VariableGroups []adoVariableGroup     `json:"variableGroups,omitempty"`
}

type adoPipelineRepository struct {
	ID   string `json:"id"`
	Type string `json:"type"`
}

type adoRunPipelineRequest struct {
	Resources          adoRunPipelineResources `json:"resources"`
	TemplateParameters map[string]string       `json:"templateParameters,omitempty"`
	Variables          map[string]adoVariable  `json:"variables,omitempty"`
}

type adoRunPipelineResources struct {
	Repositories adoRunPipelineResourcesRepos `json:"repositories"`
}

type adoRunPipelineResourcesRepos struct {
	Self adoRunPipelineSelfRepo `json:"self"`
}

type adoRunPipelineSelfRepo struct {
	RefName string `json:"refName"`
}

func parseRunPipelineArgs(args map[string]interface{}) (adoRunPipelineParams, error) {
	var params adoRunPipelineParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return params, fmt.Errorf("parsing run pipeline args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 {
		return params, fmt.Errorf("organization, project, and pipeline_id are required")
	}

	// RefName is supplied by the caller via the args["_ref_name"] key, which
	// the consumer (registration closure) populates by running args["branch"]
	// through PipelineFormatter.FormatBranchRef. Keeping Branch and RefName
	// distinct lets the confirmation prompt show the user-facing branch name
	// while the API request uses the fully qualified ref.
	if params.RefName == "" {
		// Defensive fallback: if no caller pre-formatted, treat Branch verbatim
		// so direct unit tests of RunPipeline still produce a non-empty ref.
		params.RefName = params.Branch
	}
	params.MappedVariables = mapADOVariables(params.Variables)

	return params, nil
}

func mapADOVariables(vars map[string]string) map[string]adoVariable {
	mapped := make(map[string]adoVariable, len(vars))
	for k, v := range vars {
		mapped[k] = adoVariable{
			Value:    v,
			IsSecret: false,
		}
	}
	return mapped
}

type adoPipelineRun struct {
	Id         int    `json:"id"`
	Name       string `json:"buildNumber"`
	State      string `json:"status"`
	Result     string `json:"result"`
	Created    string `json:"queueTime"`
	Repository struct {
		Id   string `json:"id"`
		Name string `json:"name"`
	} `json:"repository"`
}

type adoPipeline struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

// adoPipelineRunDetail holds the decoded fields for a single pipeline run.
type adoPipelineRunDetail struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Result  string `json:"result"`
	Created string `json:"createdDate"`
	Url     string `json:"url"`
}

type adoGetBuildChangesParams struct {
	Organization string `json:"organization"`
	Project      string `json:"project"`
	BuildId      int    `json:"build_id"`
	Top          int    `json:"top"`
}

func parseGetBuildChangesArgs(args map[string]interface{}) (adoGetBuildChangesParams, error) {
	var params adoGetBuildChangesParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return params, fmt.Errorf("parsing get build changes args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return params, fmt.Errorf("organization, project, and build_id are required")
	}

	if params.Top <= 0 {
		params.Top = 50
	} else if params.Top > 1000 {
		params.Top = 1000 // Defensive upper bound
	}

	return params, nil
}

type adoUpdateBuildDefParams struct {
	Organization string                 `json:"organization"`
	Project      string                 `json:"project"`
	DefinitionId int                    `json:"definition_id"`
	Variables    map[string]adoVariable `json:"variables"`
}

func parseUpdateBuildDefArgs(args map[string]interface{}) (adoUpdateBuildDefParams, error) {
	var params adoUpdateBuildDefParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return params, fmt.Errorf("parsing update build definition variables args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.DefinitionId == 0 || len(params.Variables) == 0 {
		return params, fmt.Errorf("organization, project, definition_id, and non-empty variables are required")
	}

	return params, nil
}

func filterAndLimitRuns(runs []adoPipelineRun, repoFilter string, limit int) []adoPipelineRun {
	result := runs
	if repoFilter != "" {
		var filtered []adoPipelineRun
		filter := strings.ToLower(repoFilter)
		for _, run := range runs {
			if strings.Contains(strings.ToLower(run.Repository.Name), filter) ||
				strings.Contains(strings.ToLower(run.Repository.Id), filter) {
				filtered = append(filtered, run)
			}
		}
		result = filtered
	}

	if len(result) > limit {
		result = result[:limit]
	}
	return result
}

// adoLogEntry is the metadata for a single log file in a pipeline run.
type adoLogEntry struct {
	Id   int    `json:"id"`
	Url  string `json:"url"`
	Line int    `json:"lineCount"`
}

// logContent is the result of fetching log text. If Truncated is true, Content
// has been clipped after TotalLines lines.
type logContent struct {
	Content    string
	Truncated  bool
	TotalLines int
}

// runPipelineResult describes the outcome of an attempted pipeline trigger.
// If Cancelled is true, the user declined the confirmation prompt and no run
// was created. Otherwise, RunID and WebURL describe the new run.
type runPipelineResult struct {
	Cancelled bool
	RunID     int
	WebURL    string
}

// createPipelineResult describes the outcome of an attempted pipeline creation.
// Exactly one of the following states is signalled:
//   - AlreadyExisted=true, PipelineID set:  idempotency hit; no creation occurred.
//   - Cancelled=true,      PipelineID==0:   user declined the confirmation prompt.
//   - both false,          PipelineID set:  pipeline was newly created.
//
// Name is always echoed so the consumer can render either the "already exists"
// or "successfully created" message without re-reading args.
type createPipelineResult struct {
	AlreadyExisted bool
	Cancelled      bool
	PipelineID     int
	Name           string
}

// UpdateVariablesResult describes the outcome of an attempted build-definition
// variables update. If Cancelled is true, the user declined the confirmation
// prompt and no PUT request was sent.
type UpdateVariablesResult struct {
	Cancelled    bool
	DefinitionID int
}

// registerPipelines registers the Pipeline category tools (list, run, definition, logs, create).
func registerPipelines(r tools.Registry, m *AdoManager, f PipelineFormatter) error {
	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
	}

	specs := []toolSpec{
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
				return tools.ToolResult{Text: f.FormatPipelineList(pipelines)}, nil
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
				return tools.ToolResult{Text: f.FormatPipelineRunsList(pipelineID, runs)}, nil
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
				return tools.ToolResult{Text: f.FormatPipelineRunDetail(run)}, nil
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
				var dispatch struct {
					LogId int `json:"log_id"`
				}
				_ = tools.UnmarshalArgs(args, &dispatch)

				if dispatch.LogId == 0 {
					runID, logs, err := m.ListPipelineLogs(ctx, args)
					if err != nil {
						return tools.ToolResult{}, err
					}
					return tools.ToolResult{Text: f.FormatPipelineLogList(runID, logs)}, nil
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
				branchRaw, _ := args["branch"].(string)
				refName := f.FormatBranchRef(branchRaw)

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
