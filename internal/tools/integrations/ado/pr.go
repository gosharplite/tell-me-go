// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

func (m *AdoManager) AdoGetPullRequest(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pull request args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get pull request request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var prData adoPullRequestDetail
	if err := json.NewDecoder(resp.Body).Decode(&prData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools.ToolResult{Text: m.formatPullRequestDetail(params.PullRequestId, prData)}, nil
}

type adoPullRequestDetail struct {
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

func (m *AdoManager) formatPullRequestDetail(pullRequestId int, prData adoPullRequestDetail) string {
	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Pull Request #%d: %s\n", pullRequestId, prData.Title)
	_, _ = fmt.Fprintf(&resultText, "Status: %s\n", prData.Status)
	_, _ = fmt.Fprintf(&resultText, "Created By: %s\n", prData.CreatedBy.DisplayName)
	_, _ = fmt.Fprintf(&resultText, "Created At: %s\n", prData.CreationDate)
	_, _ = fmt.Fprintf(&resultText, "Source: %s\n", prData.SourceRefName)
	_, _ = fmt.Fprintf(&resultText, "Target: %s\n", prData.TargetRefName)
	_, _ = fmt.Fprintf(&resultText, "Merge Status: %s\n", prData.MergeStatus)
	_, _ = fmt.Fprintf(&resultText, "Repository: %s (%s)", prData.Repository.Name, prData.Repository.Id)

	return resultText.String()
}

func (m *AdoManager) AdoListPullRequests(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		Status       string `json:"status"`
		Top          int    `json:"top"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing list pull requests args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and repository are required")
	}

	requestURL, err := m.buildListPullRequestsURL(params.Organization, params.Project, params.Repository, params.Status, params.Top)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("building list pull requests URL: %w", err)
	}

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing list pull requests request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var responseData struct {
		Value []adoPullRequestShort `json:"value"`
		Count int                   `json:"count"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&responseData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools.ToolResult{Text: m.formatPullRequestsList(responseData.Value)}, nil
}

type adoPullRequestShort struct {
	PullRequestId int    `json:"pullRequestId"`
	Title         string `json:"title"`
	CreatedBy     struct {
		DisplayName string `json:"displayName"`
	} `json:"createdBy"`
	CreationDate string `json:"creationDate"`
}

func (m *AdoManager) buildListPullRequestsURL(org, project, repo, status string, top int) (string, error) {
	if status == "" {
		status = "active"
	}
	if top <= 0 {
		top = 50
	} else if top > 1000 {
		top = 1000 // Defensive upper bound
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo)))
	if err != nil {
		return "", fmt.Errorf("failed to parse base URL: %w", err)
	}

	q := u.Query()
	q.Set("searchCriteria.status", status)
	q.Set("$top", strconv.Itoa(top))
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	return u.String(), nil
}

func (m *AdoManager) formatPullRequestsList(prs []adoPullRequestShort) string {
	if len(prs) == 0 {
		return "No pull requests found."
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Found %d pull requests:\n\n", len(prs))
	for _, pr := range prs {
		_, _ = fmt.Fprintf(&resultText, "- [#%d] %s (Created by: %s, Date: %s)\n",
			pr.PullRequestId, pr.Title, pr.CreatedBy.DisplayName, pr.CreationDate)
	}

	return resultText.String()
}

func (m *AdoManager) AdoGetPrDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pr diff args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/iterations/1/changes?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get pr diff request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
	_, _ = fmt.Fprintf(&resultText, "Pull Request #%d Changes Summary:\n", params.PullRequestId)
	_, _ = fmt.Fprintf(&resultText, "Total files changed: %d\n\n", len(changeData.ChangeEntries))

	for _, entry := range changeData.ChangeEntries {
		changeType := cases.Title(language.English).String(entry.ChangeType)
		_, _ = fmt.Fprintf(&resultText, "- [%s] %s\n", changeType, entry.Item.Path)
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

func (m *AdoManager) AdoGetPrThreads(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pr threads args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/threads?api-version=7.1",
		m.BaseURL, url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("executing get pr threads request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var threadData adoThreadResponse
	if err := json.NewDecoder(resp.Body).Decode(&threadData); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to decode response: %w", err)
	}

	return tools.ToolResult{Text: m.formatPrThreads(params.PullRequestId, threadData)}, nil
}

func (m *AdoManager) formatPrThreads(pullRequestId int, threadData adoThreadResponse) string {
	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Pull Request #%d Discussion Threads:\n\n", pullRequestId)

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
		_, _ = fmt.Fprintf(&resultText, "--- Thread %d ---\n", threadCount)
		for _, comment := range thread.Comments {
			if comment.Content == "" {
				continue
			}
			_, _ = fmt.Fprintf(&resultText, "[%s] %s: %s\n", comment.PublishedDate, comment.Author.DisplayName, comment.Content)
		}
		resultText.WriteString("\n")
	}

	if threadCount == 0 {
		return "No discussion threads found in this pull request."
	}

	return resultText.String()
}

// registerPullRequests registers the Pull Request category tools.
// Note: AdoGetPrStatuses and AdoGetPrPolicyEvaluations handlers live in policy.go
// but are registered here because they are PR-scoped tools.
func registerPullRequests(r tools.Registry, m *AdoManager, _ PipelineFormatter) error {
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
	}

	for _, spec := range specs {
		if err := r.RegisterToToolkit("ado", spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
