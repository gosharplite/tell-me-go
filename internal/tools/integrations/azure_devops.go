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
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

type azureDevOpsManager struct {
	sm     *security.SecurityManager
	client tools.HTTPClient
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
	pat := os.Getenv("AZURE_PAT_ALL")
	if pat == "" {
		return "", fmt.Errorf("missing AZURE_PAT_ALL environment variable")
	}
	auth := fmt.Sprintf(":%s", pat)
	encodedAuth := base64.StdEncoding.EncodeToString([]byte(auth))
	return fmt.Sprintf("Basic %s", encodedAuth), nil
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to access this pull request")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("pull request not found: %d", params.PullRequestId)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

	var prData struct {
		Title     string `json:"title"`
		Status    string `json:"status"`
		CreatedBy struct {
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to list pull requests")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("repository not found: %s", params.Repository)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d/iterations/1/changes?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to access this pull request")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("endpoint or pull request not found (404). URL: %s", requestURL)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

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
		changeType := strings.Title(entry.ChangeType)
		resultText.WriteString(fmt.Sprintf("- [%s] %s\n", changeType, entry.Item.Path))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests/%d/threads?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to access this pull request")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("pull request not found: %d", params.PullRequestId)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

	var threadData struct {
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

	if err := json.NewDecoder(resp.Body).Decode(&threadData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d Discussion Threads:\n\n", params.PullRequestId))

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
		return tools.ToolResult{Text: "No discussion threads found in this pull request."}, nil
	}

	return tools.ToolResult{Text: resultText.String()}, nil
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to access this file")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("file not found: %s at version %s", params.Path, params.Version)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return tools.ToolResult{}, fmt.Errorf("unauthorized: check your AZURE_PAT_ALL")
		case http.StatusForbidden:
			return tools.ToolResult{}, fmt.Errorf("forbidden: you don't have permission to access this repository")
		case http.StatusNotFound:
			return tools.ToolResult{}, fmt.Errorf("repository or path not found: %s", params.ScopePath)
		default:
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}
	}

	var responseData struct {
		Value []struct {
			Path     string `json:"path"`
			IsFolder bool   `json:"isFolder"`
		} `json:"value"`
		Count int `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No items found."}, nil
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Items in %s (%s):\n\n", params.ScopePath, params.Version))
	for _, item := range responseData.Value {
		// Skip the scope path itself if it's the first element
		if item.Path == params.ScopePath && len(responseData.Value) > 1 {
			continue
		}
		prefix := "[FILE]"
		if item.IsFolder {
			prefix = "[DIR] "
		}
		resultText.WriteString(fmt.Sprintf("- %s %s\n", prefix, item.Path))
	}

	return tools.ToolResult{Text: resultText.String()}, nil
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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
	}

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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	requestURL := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", authHeader)
	req.Header.Set("Accept", "application/json")

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
	}

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

	authHeader, err := m.getAuthHeader()
	if err != nil {
		return tools.ToolResult{}, err
	}

	if params.LogId == 0 {
		// List logs first
		u := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d/logs?api-version=7.1",
			url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("failed to create list logs request: %w", err)
		}
		req.Header.Set("Authorization", authHeader)
		req.Header.Set("Accept", "application/json")

		resp, err := m.client.Do(req)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
		}

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
		resultText.WriteString(fmt.Sprintf("Logs for Pipeline Run #%d:\n\n", params.RunId))
		for _, log := range logsData.Value {
			resultText.WriteString(fmt.Sprintf("- Log ID: %d (%d lines)\n", log.Id, log.Line))
		}
		resultText.WriteString("\nPlease provide a log_id to fetch specific log content.")
		return tools.ToolResult{Text: resultText.String()}, nil
	}

	// Fetch specific log content
	u := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/pipelines/%d/runs/%d/logs/%d?api-version=7.1",
		url.PathEscape(params.Organization), url.PathEscape(params.Project), params.PipelineId, params.RunId, params.LogId)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create log content request: %w", err)
	}
	req.Header.Set("Authorization", authHeader)

	resp, err := m.client.Do(req)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return tools.ToolResult{}, fmt.Errorf("azure DevOps API returned status: %s, body: %s", resp.Status, string(body))
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read log content: %w", err)
	}

	return tools.ToolResult{Text: string(content)}, nil
}
