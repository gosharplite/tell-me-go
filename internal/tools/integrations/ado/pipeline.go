// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

var defaultPipelineFormatter = NewPipelineFormatter()

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

	// Computed
	RefName         string
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

	params.RefName = defaultPipelineFormatter.FormatBranchRef(params.Branch)
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

// LogContent is the result of fetching log text. If Truncated is true, Content
// has been clipped after TotalLines lines.
type LogContent struct {
	Content    string
	Truncated  bool
	TotalLines int
}
