// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

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

func (m *adoManager) adoGetPrStatuses(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pr statuses args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	statusData, err := m.fetchPrStatuses(ctx, params.Organization, params.Project, params.Repository, params.PullRequestId)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching pr statuses: %w", err)
	}

	return tools.ToolResult{Text: m.formatPrStatuses(params.PullRequestId, statusData)}, nil
}

func (m *adoManager) fetchPrStatuses(ctx context.Context, org, project, repo string, prID int) (adoStatusResponse, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/statuses",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo), prID))
	if err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to parse statuses base URL: %w", err)
	}

	q := u.Query()
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.executeRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return adoStatusResponse{}, fmt.Errorf("executing fetch pr statuses request: %w", err)
	}
	defer resp.Body.Close()

	var statusData adoStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return statusData, nil
}

func (m *adoManager) formatPrStatuses(pullRequestId int, statusData adoStatusResponse) string {
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

func (m *adoManager) adoGetPrPolicyEvaluations(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization  string `json:"organization"`
		Project       string `json:"project"`
		Repository    string `json:"repository"`
		PullRequestId int    `json:"pull_request_id"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing get pr policy evaluations args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.PullRequestId == 0 {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and pull_request_id are required")
	}

	projectID, err := m.fetchPrProjectID(ctx, params.Organization, params.Project, params.Repository, params.PullRequestId)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching pr project ID: %w", err)
	}

	targetId := fmt.Sprintf("vstfs:///CodeReview/CodeReviewId/%s/%d", projectID, params.PullRequestId)

	policyData, err := m.fetchPolicyEvaluations(ctx, params.Organization, params.Project, targetId)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching policy evaluations: %w", err)
	}

	return m.formatPolicyEvaluations(params.PullRequestId, policyData)
}

func (m *adoManager) fetchPrProjectID(ctx context.Context, org, project, repo string, prID int) (string, error) {
	prRequestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo), prID)

	resp, err := m.executeRequest(ctx, http.MethodGet, prRequestURL, nil, nil)
	if err != nil {
		return "", fmt.Errorf("executing fetch pr project ID request: %w", err)
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
		return "", fmt.Errorf("failed to decode PR metadata: %w", err)
	}

	if prData.Repository.Project.Id == "" {
		return "", fmt.Errorf("could not find project ID for pull request #%d", prID)
	}

	return prData.Repository.Project.Id, nil
}

func (m *adoManager) fetchPolicyEvaluations(ctx context.Context, org, project, artifactID string) (adoPolicyResponse, error) {
	baseURL := fmt.Sprintf("%s/%s/%s/_apis/policy/evaluations",
		m.baseURL, url.PathEscape(org), url.PathEscape(project))

	u, err := url.Parse(baseURL)
	if err != nil {
		return adoPolicyResponse{}, fmt.Errorf("failed to parse policy base URL: %w", err)
	}

	q := u.Query()
	q.Set("artifactId", artifactID)
	q.Set("api-version", "7.1-preview.1")
	u.RawQuery = q.Encode()

	return m.performPolicyEvaluationRequest(ctx, u.String())
}

func (m *adoManager) performPolicyEvaluationRequest(ctx context.Context, requestURL string) (adoPolicyResponse, error) {
	resp, err := m.executeRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return adoPolicyResponse{}, fmt.Errorf("performing policy evaluation request: %w", err)
	}
	defer resp.Body.Close()

	var policyData adoPolicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policyData); err != nil {
		return adoPolicyResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return policyData, nil
}

func (m *adoManager) formatPolicyEvaluations(pullRequestId int, policyData adoPolicyResponse) (tools.ToolResult, error) {
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

func (m *adoManager) adoListBranchPolicies(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Organization string `json:"organization"`
		Project      string `json:"project"`
		Repository   string `json:"repository"`
		BranchName   string `json:"branch_name"`
	}

	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("parsing list branch policies args: %w", err)
	}

	if params.Organization == "" || params.Project == "" || params.Repository == "" || params.BranchName == "" {
		return tools.ToolResult{}, fmt.Errorf("organization, project, repository, and branch_name are required")
	}

	targetRepoId, err := m.fetchRepositoryId(ctx, params.Organization, params.Project, params.Repository)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching repository ID: %w", err)
	}

	policyConfigs, err := m.fetchPolicyConfigurations(ctx, params.Organization, params.Project)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("fetching policy configurations: %w", err)
	}

	return tools.ToolResult{Text: m.formatBranchPolicies(params.BranchName, params.Repository, policyConfigs, targetRepoId)}, nil
}

func (m *adoManager) fetchRepositoryId(ctx context.Context, org, project, repo string) (string, error) {
	repoURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s?api-version=7.1",
		m.baseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo))

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

func (m *adoManager) fetchPolicyConfigurations(ctx context.Context, org, project string) ([]adoPolicyConfig, error) {
	policyURL := fmt.Sprintf("%s/%s/%s/_apis/policy/configurations?api-version=7.1",
		m.baseURL, url.PathEscape(org), url.PathEscape(project))

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

func (m *adoManager) formatBranchPolicies(branchName, repositoryName string, policyConfigs []adoPolicyConfig, targetRepoId string) string {
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

func (m *adoManager) policyMatchesBranch(config adoPolicyConfig, targetRepoId, fullBranchRef string) bool {
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
