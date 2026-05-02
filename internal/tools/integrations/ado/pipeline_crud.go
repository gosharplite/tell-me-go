// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"bytes"
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

func (m *AdoManager) fetchPipelines(ctx context.Context, org, project string) ([]adoPipeline, error) {
	cacheKey := org + "/" + project

	// 1. Check Cache (Fast Path)
	if val, ok := m.pipelineCache.Load(cacheKey); ok {
		return val.([]adoPipeline), nil
	}

	// 2. Use Singleflight to prevent stampede
	val, err, _ := m.pipelineFetchGroup.Do(cacheKey, func() (interface{}, error) {
		// Re-check cache in case another request finished while we were waiting for singleflight
		if val, ok := m.pipelineCache.Load(cacheKey); ok {
			return val, nil
		}

		requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines?api-version=7.1",
			m.BaseURL, url.PathEscape(org), url.PathEscape(project))

		resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("executing fetch pipelines request: %w", err)
		}
		defer func() { _ = resp.Body.Close() }()

		var responseData struct {
			Value []adoPipeline `json:"value"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// 3. Write to Cache
		m.pipelineCache.Store(cacheKey, responseData.Value)

		return responseData.Value, nil
	})

	if err != nil {
		return nil, err
	}

	return val.([]adoPipeline), nil
}

// ListPipelines is the infrastructure-layer entry point for fetching pipeline
// definitions for a given org/project. Returns raw domain structs.
func (m *AdoManager) ListPipelines(ctx context.Context, args map[string]interface{}) ([]adoPipeline, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return nil, fmt.Errorf("parsing list pipelines args: %w", err)
	}

	if params.Organization == "" || params.Project == "" {
		return nil, fmt.Errorf("organization and project are required")
	}

	pipelines, err := m.fetchPipelines(ctx, params.Organization, params.Project)
	if err != nil {
		return nil, fmt.Errorf("fetching pipelines: %w", err)
	}

	return pipelines, nil
}

func (m *AdoManager) resolvePipelineID(ctx context.Context, org, project, pipelineName string) (int, error) {
	pipelines, err := m.fetchPipelines(ctx, org, project)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch pipelines for name resolution: %w", err)
	}

	for _, p := range pipelines {
		if strings.Contains(strings.ToLower(p.Name), strings.ToLower(pipelineName)) {
			return p.Id, nil
		}
	}

	return 0, fmt.Errorf("pipeline with name '%s' not found", pipelineName)
}

func (m *AdoManager) AdoCreatePipeline(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	params, err := parseCreatePipelineArgs(args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing create pipeline args: %w", err)
	}

	// 1. Idempotency Check
	existingID, err := m.checkPipelineExists(ctx, params.Organization, params.Project, params.Name)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("checking pipeline existence: %w", err)
	}
	if existingID != 0 {
		return tools.ToolResult{Text: fmt.Sprintf("Pipeline '%s' already exists with ID: %d", params.Name, existingID)}, nil
	}

	// 2. Confirm Action
	approved, err := m.sc.Confirm(ctx, fmt.Sprintf("Create pipeline '%s' in %s/%s?", params.Name, params.Organization, params.Project))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("confirmation error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Pipeline creation cancelled by user."}, nil
	}

	// 3. Create Pipeline
	pipelineID, err := m.executeCreatePipeline(ctx, params.Organization, params.Project, params.Name, params.RepositoryId, params.YamlPath, params.Variables, params.VariableGroups)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing create pipeline: %w", err)
	}

	// Invalidate cache for this project since we created a new pipeline
	m.pipelineCache.Delete(params.Organization + "/" + params.Project)

	return tools.ToolResult{Text: fmt.Sprintf("Successfully created pipeline '%s' with ID: %d", params.Name, pipelineID)}, nil
}

func (m *AdoManager) checkPipelineExists(ctx context.Context, org, project, name string) (int, error) {
	pipelines, err := m.fetchPipelines(ctx, org, project)
	if err != nil {
		return 0, fmt.Errorf("failed to check existing pipelines: %w", err)
	}

	for _, p := range pipelines {
		if strings.EqualFold(p.Name, name) {
			return p.Id, nil
		}
	}
	return 0, nil
}

func (m *AdoManager) executeCreatePipeline(ctx context.Context, org, project, name, repoID, yamlPath string, variables map[string]adoVariable, variableGroups []int) (int, error) {
	var groups []adoVariableGroup
	for _, id := range variableGroups {
		groups = append(groups, adoVariableGroup{ID: id})
	}

	// Default variables to allow override at queue time if not specified
	for k, v := range variables {
		if v.AllowOverride == nil {
			trueVal := true
			v.AllowOverride = &trueVal
		}
		// Sync IsSettableAtQueueTime with AllowOverride for compatibility across ADO APIs
		v.IsSettableAtQueueTime = v.AllowOverride
		variables[k] = v
	}

	payload := adoCreatePipelineRequest{
		Name: name,
		Configuration: adoPipelineConfiguration{
			Type: "yaml",
			Path: yamlPath,
			Repository: adoPipelineRepository{
				ID:   repoID,
				Type: "azureReposGit",
			},
			Variables:      variables,
			VariableGroups: groups,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal payload: %w", err)
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines?api-version=7.1-preview.1",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project))

	resp, err := m.ExecuteRequest(ctx, http.MethodPost, requestURL, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return 0, fmt.Errorf("executing create pipeline request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var newPipeline struct {
		Id int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&newPipeline); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return newPipeline.Id, nil
}

// GetPipelineDefinition is the infrastructure-layer entry point for fetching
// the raw ADO pipeline configuration. Returns the decoded JSON payload as an
// untyped value; the caller is responsible for serialization.
func (m *AdoManager) GetPipelineDefinition(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return nil, fmt.Errorf("parsing get pipeline definition args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 {
		return nil, fmt.Errorf("organization, project, and pipeline_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d?api-version=7.1-preview.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("executing get pipeline definition request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var definition interface{}
	if err := json.NewDecoder(resp.Body).Decode(&definition); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return definition, nil
}

func buildVariablesUpdatePayload(existingDef map[string]interface{}, inputVars map[string]adoVariable) ([]byte, error) {
	vars, ok := existingDef["variables"].(map[string]interface{})
	if !ok {
		vars = make(map[string]interface{})
		existingDef["variables"] = vars
	}

	for k, v := range inputVars {
		varObj := map[string]interface{}{
			"value":    v.Value,
			"isSecret": v.IsSecret,
		}
		if v.AllowOverride != nil {
			varObj["allowOverride"] = *v.AllowOverride
		}
		vars[k] = varObj
	}

	return json.Marshal(existingDef)
}

func (m *AdoManager) AdoUpdateBuildDefinitionVariables(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	params, err := parseUpdateBuildDefArgs(args)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 1. GET current definition
	u := fmt.Sprintf("%s/%s/%s/_apis/build/definitions/%d?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.DefinitionId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching build definition: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var definition map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&definition); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode definition: %w", err)
	}

	// 2. BUILD updated payload
	body, err := buildVariablesUpdatePayload(definition, params.Variables)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to build updated payload: %w", err)
	}

	// 3. Confirm Action
	approved, err := m.sc.Confirm(ctx, fmt.Sprintf("Update variables for build definition %d in %s/%s?", params.DefinitionId, params.Organization, params.Project))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("confirmation error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Update cancelled by user."}, nil
	}

	// 4. PUT updated definition
	putResp, err := m.ExecuteRequest(ctx, http.MethodPut, u, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing update build definition variables request: %w", err)
	}
	defer func() { _ = putResp.Body.Close() }()

	return tools.ToolResult{Text: fmt.Sprintf("Successfully updated variables for build definition %d", params.DefinitionId)}, nil
}

func (m *AdoManager) buildGetBuildChangesURL(params adoGetBuildChangesParams) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/changes",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("$top", strconv.Itoa(params.Top))
	q.Set("api-version", "7.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (m *AdoManager) decodeBuildChangesResponse(body io.Reader) ([]interface{}, error) {
	var responseData struct {
		Value []interface{} `json:"value"`
	}

	if err := json.NewDecoder(body).Decode(&responseData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return responseData.Value, nil
}

// GetBuildChanges is the infrastructure-layer entry point for fetching the
// changeset associated with an ADO build. Returns the decoded values slice; the
// caller is responsible for serialization.
func (m *AdoManager) GetBuildChanges(ctx context.Context, args map[string]interface{}) ([]interface{}, error) {
	params, err := parseGetBuildChangesArgs(args)
	if err != nil {
		return nil, err
	}

	u, err := m.buildGetBuildChangesURL(params)
	if err != nil {
		return nil, err
	}

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("executing get build changes request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return m.decodeBuildChangesResponse(resp.Body)
}
