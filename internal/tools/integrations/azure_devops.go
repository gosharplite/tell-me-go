// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type azureDevOpsManager struct {
	sm         *security.SecurityManager
	client     tools.HTTPClient
	authHeader string
	authErr    error
	authOnce   sync.Once
}

// NewAzureDevOpsManager creates a new instance of azureDevOpsManager.
func NewAzureDevOpsManager(sm *security.SecurityManager, client tools.HTTPClient) *azureDevOpsManager {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &azureDevOpsManager{
		sm:     sm,
		client: client,
	}
}

func (m *azureDevOpsManager) getAuthHeader() (string, error) {
	m.authOnce.Do(func() {
		pat := os.Getenv("AZURE_PAT_ALL")
		if pat == "" {
			m.authErr = fmt.Errorf("missing AZURE_PAT_ALL environment variable")
			return
		}
		auth := fmt.Sprintf(":%s", pat)
		encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
		m.authHeader = fmt.Sprintf("Basic %s", encodedAuth)
	})
	return m.authHeader, m.authErr
}

func (m *azureDevOpsManager) executeRequest(ctx context.Context, method, requestURL string, body io.Reader, headers map[string]string) (*http.Response, error) {
	authHeader, err := m.getAuthHeader()
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)

	hasAccept := false
	for k, v := range headers {
		req.Header.Set(k, v)
		if strings.EqualFold(k, "Accept") {
			hasAccept = true
		}
	}

	if !hasAccept {
		req.Header.Set("Accept", "application/json")
	}

	resp, err := m.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		respBody, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return nil, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return nil, fmt.Errorf("forbidden: you don't have permission to access this resource")
		case http.StatusNotFound:
			return nil, fmt.Errorf("resource not found (404): %s", requestURL)
		default:
			return nil, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(respBody))
		}
	}

	return resp, nil
}

func (m *azureDevOpsManager) adoGetPullRequest(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var prData struct {
		ArtifactId string `json:"artifactId"`
		Title      string `json:"title"`
		Status     string `json:"status"`
		CreatedBy  struct {
			DisplayName string `json:"displayName"`
		} `json:"createdBy"`
		CreationDate  string `json:"creationDate"`
		SourceRefName string `json:"sourceRefName"`
		TargetRefName string `json:"targetRefName"`
		MergeStatus   string `json:"mergeStatus"`
		Repository    struct {
			Id   string `json:"id"`
			Name string `json:"name"`
		} `json:"repository"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&prData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d: %s\n", params.PullRequestId, prData.Title))
	resultText.WriteString(fmt.Sprintf("Status: %s\n", prData.Status))
	resultText.WriteString(fmt.Sprintf("Created By: %s\n", prData.CreatedBy.DisplayName))
	resultText.WriteString(fmt.Sprintf("Created At: %s\n", prData.CreationDate))
	resultText.WriteString(fmt.Sprintf("Source: %s\n", prData.SourceRefName))
	resultText.WriteString(fmt.Sprintf("Target: %s\n", prData.TargetRefName))
	resultText.WriteString(fmt.Sprintf("Merge Status: %s\n", prData.MergeStatus))
	resultText.WriteString(fmt.Sprintf("Repository: %s (%s)", prData.Repository.Name, prData.Repository.Id))

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *azureDevOpsManager) adoListPullRequests(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		Status       string `json:"status"`
		Top          int    `json:"top"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and repository are required")
	}

	if params.Status == "" {
		params.Status = "active"
	}
	if params.Top <= 0 {
		params.Top = 50
	}

	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository)))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("searchCriteria.status", params.Status)
	q.Set("$top", strconv.Itoa(params.Top))
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var responseData struct {
		Value []struct {
			PullRequestId int    `json:"pullRequestId"`
			Title         string `json:"title"`
			CreatedBy     struct {
				DisplayName string `json:"displayName"`
			} `json:"createdBy"`
			CreationDate string `json:"creationDate"`
		} `json:"value"`
		Count int `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No pull requests found."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Found %d pull requests:\n\n", len(responseData.Value)))
	for _, pr := range responseData.Value {
		resultText.WriteString(fmt.Sprintf("- [#%d] %s (Created by: %s, Date: %s)\n",
			pr.PullRequestId, pr.Title, pr.CreatedBy.DisplayName, pr.CreationDate))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *azureDevOpsManager) adoGetPrDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d/iterations/1/changes?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var changeData struct {
		ChangeEntries []struct {
			Item struct {
				Path string `json:"path"`
			} `json:"item"`
			ChangeType string `json:"changeType"`
		} `json:"changeEntries"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&changeData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(changeData.ChangeEntries) == 0 {
		return tools.ToolResult{Text: "No changes found in this pull request."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d Changes Summary:\n", params.PullRequestId))
	resultText.WriteString(fmt.Sprintf("Total files changed: %d\n\n", len(changeData.ChangeEntries)))

	for _, entry := range changeData.ChangeEntries {
		changeType := cases.Title(language.English).String(entry.ChangeType)
		resultText.WriteString(fmt.Sprintf("- [%s] %s\n", changeType, entry.Item.Path))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

type adoThreadResponse struct {
	Value []struct {
		Comments []struct {
			Author struct {
				DisplayName string `json:"displayName"`
			} `json:"author"`
			Content       string `json:"content"`
			PublishedDate string `json:"publishedDate"`
			CommentType   string `json:"commentType"`
		} `json:"comments"`
		IsDeleted bool `json:"isDeleted"`
	} `json:"value"`
}

func (m *azureDevOpsManager) adoGetPrThreads(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d/threads?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var threadData adoThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&threadData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools.ToolResult{Text: m.formatPrThreads(params.PullRequestId, threadData)}, nil
}

func (m *azureDevOpsManager) formatPrThreads(pullRequestId int, threadData adoThreadResponse) string {
	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d Discussion Threads:\n\n", pullRequestId))

	threadCount := 0
	for _, thread := range threadData.Value {
		if thread.IsDeleted {
			continue
		}

		// Check if it's a system thread (often has only system comments)
		isSystem := true
		for _, comment := range thread.Comments {
			if comment.CommentType != "system" {
				isSystem = false
				break
			}
		}

		if isSystem {
			continue
		}

		threadCount++
		resultText.WriteString(fmt.Sprintf("--- Thread %d ---\n", threadCount))
		for _, comment := range thread.Comments {
			if comment.Content == "" {
				continue
			}
			resultText.WriteString(fmt.Sprintf("[%s] %s: %s\n", comment.PublishedDate, comment.Author.DisplayName, comment.Content))
		}
		resultText.WriteString("\n")
	}

	if threadCount == 0 {
		return "No discussion threads found in this pull request."
	}

	return resultText.String()
}

func (m *azureDevOpsManager) adoGetFileContent(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		Path         string `json:"path"`
		Version      string `json:"version"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.Path == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and path are required")
	}

	if params.Version == "" {
		params.Version = "main"
	}

	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/items",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository)))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("path", params.Path)
	q.Set("versionDescriptor.version", params.Version)
	q.Set("$format", "text")
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
}

type adoRepositoryItemsResponse struct {
	Value []struct {
		Path     string `json:"path"`
		IsFolder bool   `json:"isFolder"`
	} `json:"value"`
	Count int `json:"count"`
}

func (m *azureDevOpsManager) adoListRepositoryItems(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization   string `json:"organization"`
		Project        string `json:"project"`
		Repository     string `json:"repository"`
		ScopePath      string `json:"scope_path"`
		Version        string `json:"version"`
		RecursionLevel string `json:"recursion_level"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and repository are required")
	}

	if params.ScopePath == "" {
		params.ScopePath = "/"
	}
	if params.Version == "" {
		params.Version = "main"
	}
	if params.RecursionLevel == "" {
		params.RecursionLevel = "none"
	}

	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/items",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository)))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("scopePath", params.ScopePath)
	q.Set("versionDescriptor.version", params.Version)
	q.Set("recursionLevel", params.RecursionLevel)
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var responseData adoRepositoryItemsResponse
	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return m.formatRepositoryItems(params.ScopePath, params.Version, responseData), nil
}

func (m *azureDevOpsManager) formatRepositoryItems(scopePath, version string, responseData adoRepositoryItemsResponse) tools.ToolResult {
	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No items found."}
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Items in %s (%s):\n\n", scopePath, version))
	for _, item := range responseData.Value {
		// Skip the scope path itself if it's the first element
		if item.Path == scopePath && len(responseData.Value) > 1 {
			continue
		}
		prefix := "[FILE]"
		if item.IsFolder {
			prefix = "[DIR] "
		}
		resultText.WriteString(fmt.Sprintf("- %s %s\n", prefix, item.Path))
	}

	return tools.ToolResult{Text: resultText.String()}
}

func (m *azureDevOpsManager) adoListPipelineRuns(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		Top          int    `json:"top"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and pipeline_id are required")
	}

	if params.Top <= 0 {
		params.Top = 10
	}

	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId))
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("$top", strconv.Itoa(params.Top))
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var responseData struct {
		Value []struct {
			Id      int    `json:"id"`
			Name    string `json:"name"`
			State   string `json:"state"`
			Result  string `json:"result"`
			Created string `json:"createdDate"`
		} `json:"value"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No pipeline runs found."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Recent runs for pipeline %d:\n\n", params.PipelineId))
	for _, run := range responseData.Value {
		resultText.WriteString(fmt.Sprintf("- Run ID: %d, Name: %s, Status: %s, Result: %s, Created: %s\n",
			run.Id, run.Name, run.State, run.Result, run.Created))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *azureDevOpsManager) adoGetPipelineRun(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		PipelineId   int    `json:"pipeline_id"`
		RunId        int    `json:"run_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

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
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.PipelineId == 0 || params.RunId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, pipeline_id, and run_id are required")
	}

	if params.LogId == 0 {
		return m.listPipelineLogs(ctx, params.Organization, params.Project, params.PipelineId, params.RunId)
	}

	return m.fetchPipelineLogContent(ctx, params.Organization, params.Project, params.PipelineId, params.RunId, params.LogId)
}

func (m *azureDevOpsManager) listPipelineLogs(ctx context.Context, org, project string, pipelineId, runId int) (tools.ToolResult, error) {
	u := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d/logs?api-version=7.1",
		url.PathEscape(org), url.PathEscape(project), pipelineId, runId)

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

func (m *azureDevOpsManager) fetchPipelineLogContent(ctx context.Context, org, project string, pipelineId, runId, logId int) (tools.ToolResult, error) {
	u := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d/logs/%d?api-version=7.1",
		url.PathEscape(org), url.PathEscape(project), pipelineId, runId, logId)

	resp, err := m.executeRequest(ctx, http.MethodGet, u, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read log content: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
}

type adoStatusResponse struct {
	Value []adoStatusItem `json:"value"`
}

type adoStatusItem struct {
	State       string     `json:"state"` // succeeded, failed, pending, error
	Description string     `json:"description"`
	Context     adoContext `json:"context"`
	TargetUrl   string     `json:"targetUrl"`
	CreatedDate string     `json:"creationDate"`
}

type adoContext struct {
	Name  string `json:"name"`
	Genre string `json:"genre"`
}

func (m *azureDevOpsManager) adoGetPrStatuses(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	statusData, err := m.fetchPrStatuses(ctx, params.Organization, params.Project, params.Repository, params.PullRequestId)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: m.formatPrStatuses(params.PullRequestId, statusData)}, nil
}

func (m *azureDevOpsManager) fetchPrStatuses(ctx context.Context, org, project, repo string, prID int) (adoStatusResponse, error) {
	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d/statuses",
		url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo), prID))
	if err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to parse statuses base URL: %w", err)
	}

	q := u.Query()
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return adoStatusResponse{}, err
	}
	defer resp.Body.Close()

	var statusData adoStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return statusData, nil
}

func (m *azureDevOpsManager) formatPrStatuses(pullRequestId int, statusData adoStatusResponse) string {
	if len(statusData.Value) == 0 {
		return "No statuses found for this pull request."
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d Statuses:\n\n", pullRequestId))

	for _, item := range statusData.Value {
		stateEmoji := getStatusEmoji(item.State)

		contextName := item.Context.Name
		if item.Context.Genre != "" {
			contextName = fmt.Sprintf("%s/%s", item.Context.Genre, item.Context.Name)
		}

		resultText.WriteString(fmt.Sprintf("%s **%s**: %s\n", stateEmoji, contextName, item.State))
		if item.Description != "" {
			resultText.WriteString(fmt.Sprintf("   Description: %s\n", item.Description))
		}
		if item.TargetUrl != "" {
			resultText.WriteString(fmt.Sprintf("   Details: %s\n", item.TargetUrl))
		}
		resultText.WriteString("\n")
	}

	return resultText.String()
}

func getStatusEmoji(state string) string {
	switch strings.ToLower(state) {
	case "succeeded":
		return "✅"
	case "failed", "error":
		return "❌"
	case "pending":
		return "⏳"
	default:
		return "⚪"
	}
}

type adoPolicyResponse struct {
	Value []adoPolicyEvaluation `json:"value"`
}

type adoPolicyEvaluation struct {
	Status        string          `json:"status"` // approved, broken, rejected, queued, running
	Configuration adoPolicyConfig `json:"configuration"`
}

type adoPolicyConfig struct {
	IsEnabled  bool                   `json:"isEnabled"`
	IsBlocking bool                   `json:"isBlocking"`
	Type       adoPolicyType          `json:"type"`
	Settings   map[string]interface{} `json:"settings"`
}

type adoPolicyType struct {
	DisplayName string `json:"displayName"`
	Id          string `json:"id"`
}

func (m *azureDevOpsManager) adoGetPrPolicyEvaluations(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	// 1. Fetch Pull Request metadata to get the proper ArtifactID (targetId)
	prRequestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.executeRequest(ctx, http.MethodGet, prRequestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	var prData struct {
		Repository struct {
			Project struct {
				Id string `json:"id"`
			} `json:"project"`
		} `json:"repository"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode PR metadata: %w", err)
	}

	if prData.Repository.Project.Id == "" {
		return tools.ToolResult{}, fmt.Errorf("could not find project ID for pull request #%d", params.PullRequestId)
	}

	targetId := fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s/%d",
		prData.Repository.Project.Id, params.PullRequestId)

	// 2. Use the platform-provided ArtifactId for Policy Evaluation
	baseURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/policy/evaluations",
		url.PathEscape(params.Organization), url.PathEscape(params.Project))

	u, err := url.Parse(baseURL)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to parse policy base URL: %w", err)
	}

	q := u.Query()
	q.Set("artifactId", targetId)
	q.Set("api-version", "7.1-preview.1")
	u.RawQuery = q.Encode()

	policyData, err := m.performPolicyEvaluationRequest(ctx, u.String())
	if err != nil {
		return tools.ToolResult{}, err
	}

	return m.formatPolicyEvaluations(params.PullRequestId, policyData)
}

func (m *azureDevOpsManager) performPolicyEvaluationRequest(ctx context.Context, requestURL string) (adoPolicyResponse, error) {
	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return adoPolicyResponse{}, err
	}
	defer resp.Body.Close()

	var policyData adoPolicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policyData); err != nil {
		return adoPolicyResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return policyData, nil
}

func (m *azureDevOpsManager) formatPolicyEvaluations(pullRequestId int, policyData adoPolicyResponse) (tools.ToolResult, error) {
	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d Policy Evaluations:\n\n", pullRequestId))

	found := false
	for _, evaluation := range policyData.Value {
		if !evaluation.Configuration.IsEnabled {
			continue
		}
		found = true

		statusEmoji := "⚪"
		switch strings.ToLower(evaluation.Status) {
		case "approved":
			statusEmoji = "✅"
		case "broken", "rejected":
			statusEmoji = "❌"
		case "queued", "running":
			statusEmoji = "⏳"
		}

		requiredLabel := ""
		if evaluation.Configuration.IsBlocking {
			requiredLabel = " [REQUIRED]"
		}

		resultText.WriteString(fmt.Sprintf("%s **%s**%s: %s\n",
			statusEmoji, evaluation.Configuration.Type.DisplayName, requiredLabel, evaluation.Status))
	}

	if !found {
		return tools.ToolResult{Text: "No active policies found for this pull request."}, nil
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *azureDevOpsManager) adoListBranchPolicies(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		BranchName   string `json:"branch_name"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.BranchName == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and branch_name are required")
	}

	targetRepoId, err := m.fetchRepositoryId(ctx, params.Organization, params.Project, params.Repository)
	if err != nil {
		return tools.ToolResult{}, err
	}

	policyConfigs, err := m.fetchPolicyConfigurations(ctx, params.Organization, params.Project)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: m.formatBranchPolicies(params.BranchName, params.Repository, policyConfigs, targetRepoId)}, nil
}

func (m *azureDevOpsManager) fetchRepositoryId(ctx context.Context, org, project, repo string) (string, error) {
	repoURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s?api-version=7.1",
		url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo))

	resp, err := m.executeRequest(ctx, http.MethodGet, repoURL, nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch repository metadata: %w", err)
	}
	defer resp.Body.Close()

	var repoData struct {
		Id string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoData); err != nil {
		return "", fmt.Errorf("failed to decode repository metadata: %w", err)
	}
	return repoData.Id, nil
}

func (m *azureDevOpsManager) fetchPolicyConfigurations(ctx context.Context, org, project string) ([]adoPolicyConfig, error) {
	policyURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/policy/configurations?api-version=7.1",
		url.PathEscape(org), url.PathEscape(project))

	resp, err := m.executeRequest(ctx, http.MethodGet, policyURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch policy configurations: %w", err)
	}
	defer resp.Body.Close()

	var policyConfigs struct {
		Value []adoPolicyConfig `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&policyConfigs); err != nil {
		return nil, fmt.Errorf("failed to decode policy configurations: %w", err)
	}
	return policyConfigs.Value, nil
}

func (m *azureDevOpsManager) formatBranchPolicies(branchName, repositoryName string, policyConfigs []adoPolicyConfig, targetRepoId string) string {
	fullBranchRef := branchName
	if !strings.HasPrefix(fullBranchRef, "refs/heads/") {
		fullBranchRef = "refs/heads/" + fullBranchRef
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Branch Policies for %s in %s:\n\n", branchName, repositoryName))

	found := false
	for _, config := range policyConfigs {
		if !config.IsEnabled {
			continue
		}

		if !m.policyMatchesBranch(config, targetRepoId, fullBranchRef) {
			continue
		}

		found = true
		requiredLabel := ""
		if config.IsBlocking {
			requiredLabel = " [REQUIRED]"
		}

		resultText.WriteString(fmt.Sprintf("- Type: %s%s\n", config.Type.DisplayName, requiredLabel))
		resultText.WriteString("  Status: Enabled\n")
		resultText.WriteString("  Settings:\n")
		for k, v := range config.Settings {
			if k == "scope" {
				continue
			}
			resultText.WriteString(fmt.Sprintf("    %s: %v\n", formatKey(k), v))
		}
		resultText.WriteString("\n")
	}

	if !found {
		return fmt.Sprintf("No active policies found for branch %s in repository %s.", branchName, repositoryName)
	}

	return resultText.String()
}

func (m *azureDevOpsManager) policyMatchesBranch(config adoPolicyConfig, targetRepoId, fullBranchRef string) bool {
	scope, ok := config.Settings["scope"].([]interface{})
	if !ok {
		return false
	}

	for _, s := range scope {
		scopeMap, ok := s.(map[string]interface{})
		if !ok {
			continue
		}

		repoId, _ := scopeMap["repositoryId"].(string)
		refName, _ := scopeMap["refName"].(string)

		if repoId == targetRepoId && refName == fullBranchRef {
			return true
		}
	}
	return false
}

func formatKey(s string) string {
	if s == "" {
		return ""
	}
	var res strings.Builder
	for i, r := range s {
		if i == 0 {
			if r >= 'a' && r <= 'z' {
				res.WriteRune(r - 'a' + 'A')
			} else {
				res.WriteRune(r)
			}
		} else {
			if r >= 'A' && r <= 'Z' {
				res.WriteRune(' ')
			}
			res.WriteRune(r)
		}
	}
	result := res.String()
	if strings.HasSuffix(result, " Id") {
		result = result[:len(result)-2] + "ID"
	}
	return result
}

func (m *azureDevOpsManager) adoGetBuildTimeline(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and build_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/build/builds/%d/timeline?api-version=7.0",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId)

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
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 || params.LogId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, build_id, and log_id are required")
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/build/builds/%d/logs/%d?api-version=7.0",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId, params.LogId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, map[string]string{"Accept": "*/*"})
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read log content: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
}

func (m *azureDevOpsManager) adoGetBuildChanges(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		BuildId      int    `json:"build_id"`
		Top          int    `json:"top"`
	}

	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.BuildId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and build_id are required")
	}

	if params.Top <= 0 {
		params.Top = 50
	}

	u, err := url.Parse(fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/build/builds/%d/changes",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.BuildId))
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
