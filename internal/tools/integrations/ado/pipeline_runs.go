// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// ListPipelineRuns is the infrastructure-layer entry point for fetching pipeline
// runs. It returns raw domain structs together with the resolved pipeline ID
// (which the formatter needs to caption the output). Presentation is the
// caller's responsibility.
func (m *AdoManager) ListPipelineRuns(ctx context.Context, args map[string]interface{}) (pipelineID int, runs []adoPipelineRun, err error) {
	params, err := parseListPipelineRunsArgs(args)
	if err != nil {
		return 0, nil, fmt.Errorf("parsing list pipeline runs args: %w", err)
	}

	if params.PipelineId == 0 && params.PipelineName != "" {
		params.PipelineId, err = m.resolvePipelineID(ctx, params.Organization, params.Project, params.PipelineName)
		if err != nil {
			return 0, nil, fmt.Errorf("resolving pipeline ID: %w", err)
		}
	}

	requestURL, err := m.buildListPipelineRunsURL(params.Organization, params.Project, params.PipelineId, params.FetchTop)
	if err != nil {
		return 0, nil, fmt.Errorf("building list pipeline runs URL: %w", err)
	}

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("executing list pipeline runs request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var responseData struct {
		Value []adoPipelineRun `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return 0, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return params.PipelineId, filterAndLimitRuns(responseData.Value, params.Repository, params.OriginalTop), nil
}

func (m *AdoManager) buildListPipelineRunsURL(org, project string, pipelineId, top int) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project)))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("definitions", strconv.Itoa(pipelineId))
	q.Set("$top", strconv.Itoa(top))
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

// GetPipelineRun is the infrastructure-layer entry point for fetching the
// detail of a single pipeline run. Returns the raw domain struct.
func (m *AdoManager) GetPipelineRun(ctx context.Context, args map[string]interface{}) (*adoPipelineRunDetail, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		RunId        int    `json:"run_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return nil, fmt.Errorf("parsing get pipeline run args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return nil, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("executing get pipeline run request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var runData adoPipelineRunDetail
	if err := json.NewDecoder(resp.Body).Decode(&runData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &runData, nil
}

// runPipeline is the infrastructure-layer entry point for triggering a new
// ADO pipeline run. The confirmation dialog is part of this adapter's
// operational contract (destructive operations require user assent); a
// Cancelled result indicates the user declined.
//
// The caller SHOULD pre-format the branch into a fully qualified ADO ref via
// PipelineFormatter.FormatBranchRef and supply it under args["_ref_name"];
// args["branch"] remains the raw user-facing name shown in the confirmation
// prompt. If args["_ref_name"] is absent, runPipeline falls back to using
// args["branch"] verbatim (intended for direct unit-test callers only).
func (m *AdoManager) runPipeline(ctx context.Context, args map[string]interface{}) (runPipelineResult, error) {
	params, err := parseRunPipelineArgs(args)
	if err != nil {
		return runPipelineResult{}, fmt.Errorf("parsing run pipeline args: %w", err)
	}

	approved, err := m.sc.Confirm(ctx, fmt.Sprintf("Trigger run for pipeline %d (branch: %s) in %s/%s?", params.PipelineId, params.Branch, params.Organization, params.Project))
	if err != nil {
		return runPipelineResult{}, fmt.Errorf("confirmation error: %w", err)
	}
	if !approved {
		return runPipelineResult{Cancelled: true}, nil
	}

	runID, webURL, err := m.executeRunPipeline(ctx, params.Organization, params.Project, params.PipelineId, params.RefName, params.TemplateParameters, params.MappedVariables)
	if err != nil {
		return runPipelineResult{}, fmt.Errorf("executing run pipeline: %w", err)
	}

	return runPipelineResult{RunID: runID, WebURL: webURL}, nil
}

func (m *AdoManager) executeRunPipeline(ctx context.Context, org, project string, pipelineID int, refName string, templateParams map[string]string, variables map[string]adoVariable) (int, string, error) {
	payload := adoRunPipelineRequest{
		Resources: adoRunPipelineResources{
			Repositories: adoRunPipelineResourcesRepos{
				Self: adoRunPipelineSelfRepo{
					RefName: refName,
				},
			},
		},
		TemplateParameters: templateParams,
		Variables:          variables,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("failed to marshal payload: %w", err)
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs?api-version=7.1-preview.1",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), pipelineID)

	resp, err := m.ExecuteRequest(ctx, http.MethodPost, requestURL, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return 0, "", fmt.Errorf("executing run pipeline request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var runResponse struct {
		Id    int `json:"id"`
		Links struct {
			Web struct {
				Href string `json:"href"`
			} `json:"web"`
		} `json:"_links"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&runResponse); err != nil {
		return 0, "", fmt.Errorf("failed to decode response: %w", err)
	}

	return runResponse.Id, runResponse.Links.Web.Href, nil
}

// GetBuildTimeline is the infrastructure-layer entry point for fetching the
// timeline records of an ADO build. Returns the decoded records slice; the
// caller is responsible for serialization.
func (m *AdoManager) GetBuildTimeline(ctx context.Context, args map[string]interface{}) ([]interface{}, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return nil, fmt.Errorf("parsing get build timeline args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return nil, fmt.Errorf("organization, project, and build_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/timeline?api-version=7.0",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("executing get build timeline request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var timelineData struct {
		Records []interface{} `json:"records"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&timelineData); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return timelineData.Records, nil
}

// ListPipelineLogs is the infrastructure-layer entry point for fetching the
// log-file index of a pipeline run. Returns the raw log entries together with
// the run ID (which the formatter needs for captioning).
func (m *AdoManager) ListPipelineLogs(ctx context.Context, args map[string]interface{}) (runID int, logs []adoLogEntry, err error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		RunId        int    `json:"run_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return 0, nil, fmt.Errorf("parsing list pipeline logs args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return 0, nil, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	u := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("executing list pipeline logs request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var logsData struct {
		Value []adoLogEntry `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&logsData); err != nil {
		return 0, nil, fmt.Errorf("failed to decode logs list: %w", err)
	}

	return params.RunId, logsData.Value, nil
}

// pipelineLogParams holds the parsed arguments for fetching pipeline log content.
type pipelineLogParams struct {
	Organization string `json:"organization"`
	Project      string `json:"project"`
	PipelineId   int    `json:"pipeline_id"`
	RunId        int    `json:"run_id"`
	LogId        int    `json:"log_id"`
	TailLines    int    `json:"tail_lines"`
	HeadLines    int    `json:"head_lines"`
	FilterQuery  string `json:"filter_query"`
	ContextLines int    `json:"context_lines"`
	StartLine    int    `json:"start_line"`
	MaxLines     int    `json:"max_lines"`
}

// parsePipelineLogParams unmarshals and validates the arguments for
// getPipelineLogContent. Returns an error if required fields are missing.
func parsePipelineLogParams(args map[string]interface{}) (pipelineLogParams, error) {
	var params pipelineLogParams
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return pipelineLogParams{}, fmt.Errorf("parsing get pipeline log content args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 || params.LogId == 0 {
		return pipelineLogParams{}, fmt.Errorf("organization, project, pipeline_id, run_id, and log_id are required")
	}

	return params, nil
}

// buildPipelineLogURL constructs the Azure DevOps API URL for fetching
// the content of a single pipeline run log.
func (m *AdoManager) buildPipelineLogURL(p pipelineLogParams) string {
	return fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs/%d?api-version=7.1",
		m.BaseURL, url.PathEscape(p.Organization), url.PathEscape(p.Project),
		p.PipelineId, p.RunId, p.LogId)
}

// getPipelineLogContent is the infrastructure-layer entry point for fetching the
// content of a single pipeline log. Returns logContent describing the body and
// whether it was truncated.
func (m *AdoManager) getPipelineLogContent(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (logContent, error) {
	params, err := parsePipelineLogParams(args)
	if err != nil {
		return logContent{}, err
	}

	u := m.buildPipelineLogURL(params)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, u, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return logContent{}, fmt.Errorf("executing fetch pipeline log content request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	res, err := processLogContent(ctx, resp.Body, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines, hb)
	if err != nil {
		return logContent{}, fmt.Errorf("failed to process log content: %w", err)
	}

	return logContent(res), nil
}

// getTaskLog is the infrastructure-layer entry point for fetching a build task
// log. Returns logContent describing the body and whether it was truncated.
func (m *AdoManager) getTaskLog(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (logContent, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
		LogId        int    `json:"log_id"`
		TailLines    int    `json:"tail_lines"`
		HeadLines    int    `json:"head_lines"`
		FilterQuery  string `json:"filter_query"`
		ContextLines int    `json:"context_lines"`
		StartLine    int    `json:"start_line"`
		MaxLines     int    `json:"max_lines"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return logContent{}, fmt.Errorf("parsing get task log args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 || params.LogId == 0 {
		return logContent{}, fmt.Errorf("organization, project, build_id, and log_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/logs/%d?api-version=7.0",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId, params.LogId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return logContent{}, fmt.Errorf("executing get task log request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	res, err := processLogContent(ctx, resp.Body, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines, hb)
	if err != nil {
		return logContent{}, fmt.Errorf("failed to process log content: %w", err)
	}

	return logContent(res), nil
}
