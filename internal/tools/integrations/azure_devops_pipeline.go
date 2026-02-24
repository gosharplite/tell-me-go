// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"bufio"
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
		return params, err
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
	}

	params.FetchTop = params.OriginalTop
	if params.Repository != "" {
		params.FetchTop = 100 // Increased limit for manual filtering
	}

	return params, nil
}

func (m *azureDevOpsManager) adoListPipelineRuns(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	params, err := parseListPipelineRunsArgs(args)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if params.PipelineId == 0 && params.PipelineName != "" {
		var err error
		params.PipelineId, err = m.resolvePipelineID(ctx, params.Organization, params.Project, params.PipelineName)
		if err != nil {
			return tools.ToolResult{}, err
		}
	}

	requestURL, err := m.buildListPipelineRunsURL(params.Organization, params.Project, params.PipelineId, params.FetchTop)
	if err != nil {
		return tools.ToolResult{}, err
	}

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
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

func (m *azureDevOpsManager) buildListPipelineRunsURL(org, project string, pipelineId, top int) (string, error) {
	if top <= 0 {
		top = 10
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds",
		m.getBaseURL(), url.PathEscape(org), url.PathEscape(project)))
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

func (m *azureDevOpsManager) formatPipelineRunsList(pipelineId int, runs []adoPipelineRun) string {
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

func (m *azureDevOpsManager) adoGetPipelineRun(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		RunId        int    `json:"run_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d?api-version=7.1",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
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

func (m *azureDevOpsManager) adoGetPipelineLogs(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	if params.LogId == 0 {
		return m.listPipelineLogs(ctx, params.Organization, params.Project, params.PipelineId, params.RunId)
	}

	return m.fetchPipelineLogContent(ctx, params.Organization, params.Project, params.PipelineId, params.RunId, params.LogId, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines)
}

func (m *azureDevOpsManager) listPipelineLogs(ctx context.Context, org, project string, pipelineId, runId int) (tools.ToolResult, error) {
	u := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs?api-version=7.1",
		m.getBaseURL(), url.PathEscape(org), url.PathEscape(project), pipelineId, runId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
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

func (m *azureDevOpsManager) fetchPipelineLogContent(ctx context.Context, org, project string, pipelineId, runId, logId int, tailLines int, headLines int, filterQuery string, contextLines int, startLine int, maxLines int) (tools.ToolResult, error) {
	u := fmt.Sprintf("%s/%s/%s/_apis/pipelines/%d/runs/%d/logs/%d?api-version=7.1",
		m.getBaseURL(), url.PathEscape(org), url.PathEscape(project), pipelineId, runId, logId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	processedContent, err := m.processLogContent(resp.Body, tailLines, headLines, filterQuery, contextLines, startLine, maxLines)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to process log content: %w", err)
	}
	return tools.ToolResult{Text: processedContent}, nil
}

func (m *azureDevOpsManager) adoGetBuildTimeline(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and build_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/timeline?api-version=7.0",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
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

func (m *azureDevOpsManager) adoGetTaskLog(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 || params.LogId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, build_id, and log_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/logs/%d?api-version=7.0",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId, params.LogId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	processedContent, err := m.processLogContent(resp.Body, params.TailLines, params.HeadLines, params.FilterQuery, params.ContextLines, params.StartLine, params.MaxLines)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to process log content: %w", err)
	}
	return tools.ToolResult{Text: processedContent}, nil
}

func (m *azureDevOpsManager) processLogContent(reader io.Reader, tailLines, headLines int, filterQuery string, contextLines, startLine, maxLines int) (string, error) {
	if filterQuery != "" {
		return m.streamRegexFilter(reader, filterQuery, contextLines)
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

func (m *azureDevOpsManager) streamRegexFilter(reader io.Reader, query string, contextLines int) (string, error) {
	re, err := regexp.Compile(query)
	if err != nil {
		return "", fmt.Errorf("invalid filter_query regex: %w", err)
	}

	state := newLogFilterState(contextLines)
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	for scanner.Scan() {
		line := scanner.Text()
		if re.MatchString(line) {
			state.addMatch(line)
		} else {
			state.addNonMatch(line)
		}
		state.updateWindow(line)
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("log stream interrupted: %w", err)
	}

	if state.result.Len() == 0 {
		return "No matches found for filter_query.", nil
	}

	return state.result.String(), nil
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

func (m *azureDevOpsManager) streamPagination(reader io.Reader, startLine, maxLines int) (string, error) {
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
	for scanner.Scan() {
		count++
		if count < startLine {
			continue
		}
		if maxLines > 0 && printed >= maxLines {
			break
		}
		if printed > 0 {
			result.WriteString("\n")
		}
		result.WriteString(scanner.Text())
		printed++
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("log stream interrupted: %w", err)
	}

	if count < startLine && count > 0 {
		return fmt.Sprintf("Start line %d is beyond total lines %d.", startLine, count), nil
	}

	return result.String(), nil
}

func (m *azureDevOpsManager) streamHead(reader io.Reader, n int) (string, error) {
	scanner := bufio.NewScanner(reader)
	const maxCapacity = 1 * 1024 * 1024
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, maxCapacity)

	var result strings.Builder
	count := 0
	for scanner.Scan() && count < n {
		if count > 0 {
			result.WriteString("\n")
		}
		result.WriteString(scanner.Text())
		count++
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("log stream interrupted: %w", err)
	}

	return result.String(), nil
}

func (m *azureDevOpsManager) streamTail(reader io.Reader, n int) (string, error) {
	if n <= 0 {
		return "", nil
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
		return "", fmt.Errorf("log stream interrupted: %w", err)
	}

	if count == 0 {
		return "", nil
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

	return result.String(), nil
}

func (m *azureDevOpsManager) adoGetBuildChanges(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
		Top          int    `json:"top"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and build_id are required")
	}

	if params.Top <= 0 {
		params.Top = 50
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/build/builds/%d/changes",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("$top", strconv.Itoa(params.Top))
	q.Set("api-version", "7.0")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var responseData struct {
		Value []interface{} `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	output, err := json.MarshalIndent(responseData.Value, "", "  ")
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to marshal build changes: %w", err)
	}

	return tools.ToolResult{Text: string(output)}, nil
}

type adoPipeline struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

func (m *azureDevOpsManager) fetchPipelines(ctx context.Context, org, project string) ([]adoPipeline, error) {
	cacheKey := org + "/" + project

	// 1. Check Read Lock (Fast Path)
	m.pipelineCacheMu.RLock()
	if cached, exists := m.pipelineCache[cacheKey]; exists {
		m.pipelineCacheMu.RUnlock()
		return cached, nil
	}
	m.pipelineCacheMu.RUnlock()

	// 2. Use Singleflight to prevent stampede
	val, err, _ := m.pipelineFetchGroup.Do(cacheKey, func() (interface{}, error) {
		// Re-check cache in case another request finished while we were waiting for singleflight
		m.pipelineCacheMu.RLock()
		if cached, exists := m.pipelineCache[cacheKey]; exists {
			m.pipelineCacheMu.RUnlock()
			return cached, nil
		}
		m.pipelineCacheMu.RUnlock()

		requestURL := fmt.Sprintf("%s/%s/%s/_apis/pipelines?api-version=7.1",
			m.getBaseURL(), url.PathEscape(org), url.PathEscape(project))

		resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		var responseData struct {
			Value []adoPipeline `json:"value"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		// 3. Write to Cache
		m.pipelineCacheMu.Lock()
		if m.pipelineCache == nil {
			m.pipelineCache = make(map[string][]adoPipeline)
		}
		m.pipelineCache[cacheKey] = responseData.Value
		m.pipelineCacheMu.Unlock()

		return responseData.Value, nil
	})

	if err != nil {
		return nil, err
	}

	return val.([]adoPipeline), nil
}

func (m *azureDevOpsManager) adoListPipelines(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" {
		return tools.ToolResult{}, fmt.Errorf("organization and project are required")
	}

	pipelines, err := m.fetchPipelines(ctx, params.Organization, params.Project)
	if err != nil {
		return tools.ToolResult{}, err
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

func (m *azureDevOpsManager) resolvePipelineID(ctx context.Context, org, project, pipelineName string) (int, error) {
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
