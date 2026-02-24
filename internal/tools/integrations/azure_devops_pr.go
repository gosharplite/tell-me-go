// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

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

func (m *azureDevOpsManager) adoGetPullRequest(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

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

func (m *azureDevOpsManager) formatPullRequestDetail(pullRequestId int, prData adoPullRequestDetail) string {
	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Pull Request #%d: %s\n", pullRequestId, prData.Title))
	resultText.WriteString(fmt.Sprintf("Status: %s\n", prData.Status))
	resultText.WriteString(fmt.Sprintf("Created By: %s\n", prData.CreatedBy.DisplayName))
	resultText.WriteString(fmt.Sprintf("Created At: %s\n", prData.CreationDate))
	resultText.WriteString(fmt.Sprintf("Source: %s\n", prData.SourceRefName))
	resultText.WriteString(fmt.Sprintf("Target: %s\n", prData.TargetRefName))
	resultText.WriteString(fmt.Sprintf("Merge Status: %s\n", prData.MergeStatus))
	resultText.WriteString(fmt.Sprintf("Repository: %s (%s)", prData.Repository.Name, prData.Repository.Id))

	return resultText.String()
}

func (m *azureDevOpsManager) adoListPullRequests(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		Status       string `json:"status"`
		Top          int    `json:"top"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, and repository are required")
	}

	requestURL, err := m.buildListPullRequestsURL(params.Organization, params.Project, params.Repository, params.Status, params.Top)
	if err != nil {
		return tools.ToolResult{}, err
	}

	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return tools.ToolResult{}, err
	}
	defer resp.Body.Close()

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

func (m *azureDevOpsManager) buildListPullRequestsURL(org, project, repo, status string, top int) (string, error) {
	if status == "" {
		status = "active"
	}
	if top <= 0 {
		top = 50
	}

	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests",
		m.getBaseURL(), url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo)))
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

func (m *azureDevOpsManager) formatPullRequestsList(prs []adoPullRequestShort) string {
	if len(prs) == 0 {
		return "No pull requests found."
	}

	var resultText strings.Builder
	resultText.WriteString(fmt.Sprintf("Found %d pull requests:\n\n", len(prs)))
	for _, pr := range prs {
		resultText.WriteString(fmt.Sprintf("- [#%d] %s (Created by: %s, Date: %s)\n",
			pr.PullRequestId, pr.Title, pr.CreatedBy.DisplayName, pr.CreationDate))
	}

	return resultText.String()
}

func (m *azureDevOpsManager) adoGetPrDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/iterations/1/changes?api-version=7.1",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

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

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	requestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/threads?api-version=7.1",
		m.getBaseURL(), url.PathEscape(params.Organization), url.PathEscape(params.Project), url.PathEscape(params.Repository), params.PullRequestId)

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
