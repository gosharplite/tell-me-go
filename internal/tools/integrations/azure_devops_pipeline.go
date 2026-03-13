// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
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

	params.RefName = formatBranchRef(params.Branch)
	params.MappedVariables = mapADOVariables(params.Variables)

	return params, nil
}

func formatBranchRef(branch string) string {
	if branch == "" {
		branch = "main"
	}

	if strings.HasPrefix(branch, "refs/") {
		return branch
	}

	// Heuristic: if it looks like a version tag (vX.Y.Z), assume refs/tags/
	if strings.HasPrefix(branch, "v") && len(branch) > 1 && branch[1] >= '0' && branch[1] <= '9' {
		return "refs/tags/" + branch
	}

	return "refs/heads/" + branch
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

// LogFilterOptions configures log filtering behavior.
type LogFilterOptions struct {
	MaxLines     int
	ContextLines int
}

// FilterResult contains the processed log content and metadata.
type FilterResult struct {
	Content    string
	Truncated  bool
	TotalLines int
}

func (m *adoManager) adoListPipelineRuns(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	params, err := parseListPipelineRunsArgs(args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing list pipeline runs args: %w", err)
	}

	if params.PipelineId == 0 && params.PipelineName != "" {
		var err error
		params.PipelineId, err = m.resolvePipelineID(ctx, params.Organization, params.Project, params.PipelineName)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("resolving pipeline ID: %w", err)
		}
	}

	requestURL, err := m.buildListPipelineRunsURL(params.Organization, params.Project, params.PipelineId, params.FetchTop)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("building list pipeline runs URL: %w", err)
	}

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing list pipeline runs request: %w", err)
	}
	defer resp.Body.Close()

	var responseData struct {
		Value []adoPipelineRun `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	runs := filterAndLimitRuns(responseData.Value, params.Repository, params.OriginalTop)

	return tools.ToolResult{Text: m.formatPipelineRunsList(params.PipelineId, runs)}, nil
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

func (m *adoManager) buildListPipelineRunsURL(org, project string, pipelineId, top int) (string, error) {
	if top <= 0 {
		top = 10
	} else if top > 1000 {
		top = 1000 // Defensive upper bound
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds",
		m.baseURL, url.PathEscape(org), url.PathEscape(project)))
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

func (m *adoManager) formatPipelineRunsList(pipelineId int, runs []adoPipelineRun) string {
	if len(runs) == 0 {
		return "No pipeline runs found."
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Recent runs for pipeline %d:\n\n", pipelineId))
	for _, run := range runs {
		resultText.WriteString(fmt.Sprintf("- Run ID: %d, Name: %s, Status: %s, Result: %s, Created: %s, Repo: %s\n",
			run.Id, run.Name, run.State, run.Result, run.Created, run.Repository.Name))
	}

	return resultText.String()
}

func (m *adoManager) adoGetPipelineRun(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		RunId        int    `json:"run_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pipeline run args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d?api-version=7.1",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get pipeline run request: %w", err)
	}
	defer resp.Body.Close()

	var runData struct {
		Id      int    `json:"id"`
		Name    string `json:"name"`
		State   string `json:"state"`
		Result  string `json:"result"`
		Created string `json:"createdDate"`
		Url     string `json:"url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&runData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pipeline Run #%d Details:\n", runData.Id))
	resultText.WriteString(fmt.Sprintf("- Name: %s\n", runData.Name))
	resultText.WriteString(fmt.Sprintf("- Status: %s\n", runData.State))
	resultText.WriteString(fmt.Sprintf("- Result: %s\n", runData.Result))
	resultText.WriteString(fmt.Sprintf("- Created: %s\n", runData.Created))
	resultText.WriteString(fmt.Sprintf("- URL: %s\n", runData.Url))

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *adoManager) adoGetPipelineLogs(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
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

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pipeline logs args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	if params.LogId == 0 {
		return m.listPipelineLogs(ctx, params.Organization, params.Project, params.PipelineId, params.RunId)
	}

	return m.fetchPipelineLogContent(ctx, params.Organization, params.Project, params.PipelineId, params.RunId, params.LogId, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines)
}

func (m *adoManager) listPipelineLogs(ctx context.Context, org, project string, pipelineId, runId int) (tools.ToolResult, error) {
	u := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs?api-version=7.1",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), pipelineId, runId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing list pipeline logs request: %w", err)
	}
	defer resp.Body.Close()

	var logsData struct {
		Value []struct {
			Id   int    `json:"id"`
			Url  string `json:"url"`
			Line int    `json:"lineCount"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&logsData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode logs list: %w", err)
	}

	if len(logsData.Value) == 0 {
		return tools.ToolResult{Text: "No logs found for this run."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Logs for Pipeline Run #%d:\n\n", runId))
	for _, log := range logsData.Value {
		resultText.WriteString(fmt.Sprintf("- Log ID: %d (%d lines)\n", log.Id, log.Line))
	}
	resultText.WriteString("\nPlease provide a log_id to fetch specific log content.")
	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *adoManager) fetchPipelineLogContent(ctx context.Context, org, project string, pipelineId, runId, logId int, tailLines int, headLines int, filterQuery string, contextLines int, startLine int, maxLines int) (tools.ToolResult, error) {
	u := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs/%d?api-version=7.1",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), pipelineId, runId, logId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing fetch pipeline log content request: %w", err)
	}
	defer resp.Body.Close()

	res, err := m.processLogContent(resp.Body, tailLines, headLines, filterQuery, contextLines, startLine, maxLines)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to process log content: %w", err)
	}

	text := res.Content
	if res.Truncated {
		text += fmt.Sprintf("\n\n[NOTE to LLM: Log content was truncated after %d lines for safety. Suggest using a more specific filter_query or pagination if the relevant data is missing.]", res.TotalLines)
	}

	return tools.ToolResult{Text: text}, nil
}

func (m *adoManager) adoGetBuildTimeline(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get build timeline args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and build_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/timeline?api-version=7.0",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get build timeline request: %w", err)
	}
	defer resp.Body.Close()

	var timelineData struct {
		Records []interface{} `json:"records"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&timelineData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	output, err := json.MarshalIndent(timelineData.Records, "", "  ")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to marshal timeline records: %w", err)
	}

	return tools.ToolResult{Text: string(output)}, nil
}

func (m *adoManager) adoGetTaskLog(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
		return tools.ToolResult{}, fmt.Errorf("parsing get task log args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 || params.LogId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, build_id, and log_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/logs/%d?api-version=7.0",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId, params.LogId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get task log request: %w", err)
	}
	defer resp.Body.Close()

	res, err := m.processLogContent(resp.Body, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to process log content: %w", err)
	}

	text := res.Content
	if res.Truncated {
		text += fmt.Sprintf("\n\n[NOTE to LLM: Log content was truncated after %d lines for safety. Suggest using a more specific filter_query or pagination if the relevant data is missing.]", res.TotalLines)
	}

	return tools.ToolResult{Text: text}, nil
}

func (m *adoManager) processLogContent(reader io.Reader, tailLines, headLines int, filterQuery string, contextLines, startLine, maxLines int) (FilterResult, error) {
	if filterQuery != "" {
		return m.streamRegexFilter(reader, filterQuery, LogFilterOptions{MaxLines: maxLines, ContextLines: contextLines})
	}

	if startLine > 0 || maxLines > 0 {
		return m.streamPagination(reader, startLine, maxLines)
	}

	if headLines > 0 {
		return m.streamHead(reader, headLines)
	}

	if tailLines <= 0 {
		tailLines = 200
	}
	return m.streamTail(reader, tailLines)
}

func (m *adoManager) streamRegexFilter(reader io.Reader, query string, opts LogFilterOptions) (FilterResult, error) {
	re, err := regexp.Compile(query)
	if err != nil {
		return FilterResult{}, fmt.Errorf("invalid filter_query regex: %w", err)
	}

	state := newLogFilterState(opts.ContextLines)
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	maxMatchedLines := opts.MaxLines
	if maxMatchedLines <= 0 {
		maxMatchedLines = 1000
	}
	matchCount := 0
	truncated := false

	for scanner.Scan() {
		line := scanner.Text()
		if re.MatchString(line) {
			if matchCount >= maxMatchedLines {
				truncated = true
				break
			}
			state.addMatch(line)
			matchCount++
		} else {
			state.addNonMatch(line)
		}
		state.updateWindow(line)
	}

	if err := scanner.Err(); err != nil {
		return FilterResult{}, fmt.Errorf("log stream interrupted: %w", err)
	}

	content := state.result.String()
	if state.result.Len() == 0 {
		content = "No matches found for filter_query."
	}

	return FilterResult{
		Content:    content,
		Truncated:  truncated,
		TotalLines: matchCount,
	}, nil
}

type logFilterState struct {
	preWindow          []string
	preCount           int
	postLinesRemaining int
	lastPrintedLineNum int
	currentLineNum     int
	contextLines       int
	result             strings.Builder
}

func newLogFilterState(contextLines int) *logFilterState {
	if contextLines < 0 {
		contextLines = 5
	}
	if contextLines > 100 {
		contextLines = 100
	}
	var preWindow []string
	if contextLines > 0 {
		preWindow = make([]string, contextLines)
	}
	return &logFilterState{
		preWindow:          preWindow,
		contextLines:       contextLines,
		lastPrintedLineNum: -1,
	}
}

func (s *logFilterState) addMatch(line string) {
	s.printPreWindow()
	s.printLine(line, s.currentLineNum)
	s.postLinesRemaining = s.contextLines
}

func (s *logFilterState) addNonMatch(line string) {
	if s.postLinesRemaining > 0 {
		s.printLine(line, s.currentLineNum)
		s.postLinesRemaining--
	}
}

func (s *logFilterState) updateWindow(line string) {
	if s.contextLines > 0 {
		s.preWindow[s.preCount%s.contextLines] = line
	}
	s.preCount++
	s.currentLineNum++
}

func (s *logFilterState) printPreWindow() {
	if s.contextLines <= 0 {
		return
	}
	limitPre := s.contextLines
	if s.preCount < s.contextLines {
		limitPre = s.preCount
	}

	startPre := 0
	if s.preCount >= s.contextLines {
		startPre = s.preCount % s.contextLines
	}

	for i := 0; i < limitPre; i++ {
		lineNum := s.currentLineNum - (limitPre - i)
		s.printLine(s.preWindow[(startPre+i)%s.contextLines], lineNum)
	}
}

func (s *logFilterState) printLine(line string, lineNum int) {
	if lineNum <= s.lastPrintedLineNum {
		return
	}
	if s.lastPrintedLineNum != -1 && lineNum > s.lastPrintedLineNum+1 {
		s.result.WriteString("\n...")
	}
	if s.result.Len() > 0 {
		s.result.WriteString("\n")
	}
	s.result.WriteString(line)
	s.lastPrintedLineNum = lineNum
}

func (m *adoManager) streamPagination(reader io.Reader, startLine, maxLines int) (FilterResult, error) {
	if startLine <= 0 {
		startLine = 1
	}
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var result strings.Builder
	count := 0
	printed := 0
	truncated := false
	for scanner.Scan() {
		count++
		if count < startLine {
			continue
		}
		if maxLines > 0 && printed >= maxLines {
			truncated = true
			break
		}
		if printed > 0 {
			result.WriteString("\n")
		}
		result.WriteString(scanner.Text())
		printed++
	}

	if err := scanner.Err(); err != nil {
		return FilterResult{}, fmt.Errorf("log stream interrupted: %w", err)
	}

	content := result.String()
	if count < startLine && count > 0 {
		content = fmt.Sprintf("Start line %d is beyond total lines %d.", startLine, count)
	}

	return FilterResult{
		Content:    content,
		Truncated:  truncated,
		TotalLines: printed,
	}, nil
}

func (m *adoManager) streamHead(reader io.Reader, n int) (FilterResult, error) {
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var result strings.Builder
	count := 0
	truncated := false
	for scanner.Scan() && count < n {
		if count > 0 {
			result.WriteString("\n")
		}
		result.WriteString(scanner.Text())
		count++
	}
	if scanner.Scan() {
		truncated = true
	}

	if err := scanner.Err(); err != nil {
		return FilterResult{}, fmt.Errorf("log stream interrupted: %w", err)
	}

	return FilterResult{
		Content:    result.String(),
		Truncated:  truncated,
		TotalLines: count,
	}, nil
}

func (m *adoManager) streamTail(reader io.Reader, n int) (FilterResult, error) {
	if n <= 0 {
		return FilterResult{}, nil
	}
	if n > 10000 {
		n = 10000
	}
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	ring := make([]string, n)
	count := 0
	for scanner.Scan() {
		ring[count%n] = scanner.Text()
		count++
	}

	if err := scanner.Err(); err != nil {
		return FilterResult{}, fmt.Errorf("log stream interrupted: %w", err)
	}

	if count == 0 {
		return FilterResult{}, nil
	}

	var result strings.Builder
	start := 0
	if count > n {
		start = count % n
	}

	limit := n
	if count < n {
		limit = count
	}

	for i := 0; i < limit; i++ {
		if i > 0 {
			result.WriteString("\n")
		}
		result.WriteString(ring[(start+i)%n])
	}

	return FilterResult{
		Content:    result.String(),
		Truncated:  count > n,
		TotalLines: limit,
	}, nil
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

func (m *adoManager) buildGetBuildChangesURL(params adoGetBuildChangesParams) (string, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/changes",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("$top", strconv.Itoa(params.Top))
	q.Set("api-version", "7.0")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (m *adoManager) parseBuildChangesResponse(body io.Reader) (string, error) {
	var responseData struct {
		Value []interface{} `json:"value"`
	}

	if err := json.NewDecoder(body).Decode(&responseData); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	output, err := json.MarshalIndent(responseData.Value, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal build changes: %w", err)
	}

	return string(output), nil
}

func (m *adoManager) adoGetBuildChanges(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	params, err := parseGetBuildChangesArgs(args)
	if err != nil {
		return tools.ToolResult{}, err
	}

	u, err := m.buildGetBuildChangesURL(params)
	if err != nil {
		return tools.ToolResult{}, err
	}

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get build changes request: %w", err)
	}
	defer resp.Body.Close()

	result, err := m.parseBuildChangesResponse(resp.Body)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: result}, nil
}

type adoPipeline struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

func (m *adoManager) fetchPipelines(ctx context.Context, org, project string) ([]adoPipeline, error) {
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
			m.baseURL, url.PathEscape(org), url.PathEscape(project))

		resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
		if err != nil {
			return nil, fmt.Errorf("executing fetch pipelines request: %w", err)
		}
		defer resp.Body.Close()

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

func (m *adoManager) adoListPipelines(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing list pipelines args: %w", err)
	}

	if params.Organization == "" || params.Project == "" {
		return tools.ToolResult{}, fmt.Errorf("organization and project are required")
	}

	pipelines, err := m.fetchPipelines(ctx, params.Organization, params.Project)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching pipelines: %w", err)
	}

	if len(pipelines) == 0 {
		return tools.ToolResult{Text: "No pipelines found."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Found %d pipelines:\n", len(pipelines)))
	for _, p := range pipelines {
		resultText.WriteString(fmt.Sprintf("- [%d] %s\n", p.Id, p.Name))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *adoManager) resolvePipelineID(ctx context.Context, org, project, pipelineName string) (int, error) {
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

func (m *adoManager) adoCreatePipeline(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

func (m *adoManager) checkPipelineExists(ctx context.Context, org, project, name string) (int, error) {
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

func (m *adoManager) adoRunPipeline(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	params, err := parseRunPipelineArgs(args)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing run pipeline args: %w", err)
	}

	// Confirm Action
	approved, err := m.sc.Confirm(ctx, fmt.Sprintf("Trigger run for pipeline %d (branch: %s) in %s/%s?", params.PipelineId, params.Branch, params.Organization, params.Project))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("confirmation error: %w", err)
	}
	if !approved {
		return tools.ToolResult{Text: "Pipeline run cancelled by user."}, nil
	}

	runID, webURL, err := m.executeRunPipeline(ctx, params.Organization, params.Project, params.PipelineId, params.RefName, params.TemplateParameters, params.MappedVariables)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing run pipeline: %w", err)
	}

	return tools.ToolResult{
		Text: fmt.Sprintf("Successfully triggered pipeline run ID: %d\nWeb URL: %s", runID, webURL),
	}, nil
}

func (m *adoManager) executeCreatePipeline(ctx context.Context, org, project, name, repoID, yamlPath string, variables map[string]adoVariable, variableGroups []int) (int, error) {
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
		m.baseURL, url.PathEscape(org), url.PathEscape(project))

	resp, err := m.executeRequest(ctx, http.MethodPost, requestURL, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return 0, fmt.Errorf("executing create pipeline request: %w", err)
	}
	defer resp.Body.Close()

	var newPipeline struct {
		Id int `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&newPipeline); err != nil {
		return 0, fmt.Errorf("failed to decode response: %w", err)
	}

	return newPipeline.Id, nil
}

func (m *adoManager) executeRunPipeline(ctx context.Context, org, project string, pipelineID int, refName string, templateParams map[string]string, variables map[string]adoVariable) (int, string, error) {
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
		m.baseURL, url.PathEscape(org), url.PathEscape(project), pipelineID)

	resp, err := m.executeRequest(ctx, http.MethodPost, requestURL, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return 0, "", fmt.Errorf("executing run pipeline request: %w", err)
	}
	defer resp.Body.Close()

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

func (m *adoManager) adoGetPipelineDefinition(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pipeline definition args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and pipeline_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d?api-version=7.1-preview.1",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get pipeline definition request: %w", err)
	}
	defer resp.Body.Close()

	var definition interface{}
	if err := json.NewDecoder(resp.Body).Decode(&definition); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	output, err := json.MarshalIndent(definition, "", "  ")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to marshal definition: %w", err)
	}

	return tools.ToolResult{Text: string(output)}, nil
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

func (m *adoManager) adoUpdateBuildDefinitionVariables(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	params, err := parseUpdateBuildDefArgs(args)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// 1. GET current definition
	u := fmt.Sprintf("%s/%s/%s/_apis/build/definitions/%d?api-version=7.1",
		m.baseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), params.DefinitionId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching build definition: %w", err)
	}
	defer resp.Body.Close()

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
	putResp, err := m.executeRequest(ctx, http.MethodPut, u, bytes.NewReader(body), map[string]string{"Content-Type": "application/json"})
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing update build definition variables request: %w", err)
	}
	defer putResp.Body.Close()

	return tools.ToolResult{Text: fmt.Sprintf("Successfully updated variables for build definition %d", params.DefinitionId)}, nil
}
