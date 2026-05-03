// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

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

func (m *AdoManager) AdoGetPrStatuses(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

func (m *AdoManager) fetchPrStatuses(ctx context.Context, org, project, repo string, prID int) (adoStatusResponse, error) {
	u, err := url.Parse(fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d/statuses",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo), prID))
	if err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to parse statuses base URL: %w", err)
	}

	q := u.Query()
	q.Set("api-version", "7.1")
	u.RawQuery = q.Encode()

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, u.String(), nil, nil)
	if err != nil {
		return adoStatusResponse{}, fmt.Errorf("executing fetch pr statuses request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var statusData adoStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&statusData); err != nil {
		return adoStatusResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return statusData, nil
}

func (m *AdoManager) formatPrStatuses(pullRequestId int, statusData adoStatusResponse) string {
	if len(statusData.Value) == 0 {
		return "No statuses found for this pull request."
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Pull Request #%d Statuses:\n\n", pullRequestId)

	for _, item := range statusData.Value {
		stateEmoji := getStatusEmoji(item.State)

		contextName := item.Context.Name
		if item.Context.Genre != "" {
			contextName = fmt.Sprintf("%s/%s", item.Context.Genre, item.Context.Name)
		}

		_, _ = fmt.Fprintf(&resultText, "%s **%s**: %s\n", stateEmoji, contextName, item.State)
		if item.Description != "" {
			_, _ = fmt.Fprintf(&resultText, "   Description: %s\n", item.Description)
		}
		if item.TargetUrl != "" {
			_, _ = fmt.Fprintf(&resultText, "   Details: %s\n", item.TargetUrl)
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

func (m *AdoManager) AdoGetPrPolicyEvaluations(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

func (m *AdoManager) fetchPrProjectID(ctx context.Context, org, project, repo string, prID int) (string, error) {
	prRequestURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s/pullrequests/%d?api-version=7.1",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo), prID)

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, prRequestURL, nil, nil)
	if err != nil {
		return "", fmt.Errorf("executing fetch pr project ID request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

func (m *AdoManager) fetchPolicyEvaluations(ctx context.Context, org, project, artifactID string) (adoPolicyResponse, error) {
	baseURL := fmt.Sprintf("%s/%s/%s/_apis/policy/evaluations",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project))

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

func (m *AdoManager) performPolicyEvaluationRequest(ctx context.Context, requestURL string) (adoPolicyResponse, error) {
	resp, err := m.ExecuteRequest(ctx, http.MethodGet, requestURL, nil, nil)
	if err != nil {
		return adoPolicyResponse{}, fmt.Errorf("performing policy evaluation request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var policyData adoPolicyResponse
	if err := json.NewDecoder(resp.Body).Decode(&policyData); err != nil {
		return adoPolicyResponse{}, fmt.Errorf("failed to decode response: %w", err)
	}
	return policyData, nil
}

func (m *AdoManager) formatPolicyEvaluations(pullRequestId int, policyData adoPolicyResponse) (tools.ToolResult, error) {
	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Pull Request #%d Policy Evaluations:\n\n", pullRequestId)

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

		_, _ = fmt.Fprintf(&resultText, "%s **%s**%s: %s\n",
			statusEmoji, evaluation.Configuration.Type.DisplayName, requiredLabel, evaluation.Status)
	}

	if !found {
		return tools.ToolResult{Text: "No active policies found for this pull request."}, nil
	}

	return tools.ToolResult{Text: resultText.String()}, nil
}

func (m *AdoManager) adoListBranchPolicies(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

func (m *AdoManager) fetchRepositoryId(ctx context.Context, org, project, repo string) (string, error) {
	repoURL := fmt.Sprintf("%s/%s/%s/_apis/git/repositories/%s?api-version=7.1",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project), url.PathEscape(repo))

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, repoURL, nil, nil)
	if err != nil {
		return "", fmt.Errorf("failed to fetch repository metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var repoData struct {
		Id string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&repoData); err != nil {
		return "", fmt.Errorf("failed to decode repository metadata: %w", err)
	}
	return repoData.Id, nil
}

func (m *AdoManager) fetchPolicyConfigurations(ctx context.Context, org, project string) ([]adoPolicyConfig, error) {
	policyURL := fmt.Sprintf("%s/%s/%s/_apis/policy/configurations?api-version=7.1",
		m.BaseURL, url.PathEscape(org), url.PathEscape(project))

	resp, err := m.ExecuteRequest(ctx, http.MethodGet, policyURL, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch policy configurations: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	var policyConfigs struct {
		Value []adoPolicyConfig `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&policyConfigs); err != nil {
		return nil, fmt.Errorf("failed to decode policy configurations: %w", err)
	}
	return policyConfigs.Value, nil
}

func (m *AdoManager) formatBranchPolicies(branchName, repositoryName string, policyConfigs []adoPolicyConfig, targetRepoId string) string {
	fullBranchRef := branchName
	if !strings.HasPrefix(fullBranchRef, "refs/heads/") {
		fullBranchRef = "refs/heads/" + fullBranchRef
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Branch Policies for %s in %s:\n\n", branchName, repositoryName)

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

		_, _ = fmt.Fprintf(&resultText, "- Type: %s%s\n", config.Type.DisplayName, requiredLabel)
		resultText.WriteString("  Status: Enabled\n")
		resultText.WriteString("  Settings:\n")
		for k, v := range config.Settings {
			if k == "scope" {
				continue
			}
			_, _ = fmt.Fprintf(&resultText, "    %s: %v\n", formatKey(k), v)
		}
		resultText.WriteString("\n")
	}

	if !found {
		return fmt.Sprintf("No active policies found for branch %s in repository %s.", branchName, repositoryName)
	}

	return resultText.String()
}

func (m *AdoManager) policyMatchesBranch(config adoPolicyConfig, targetRepoId, fullBranchRef string) bool {
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

// registerPolicy registers the Policy category tools (branch policies, build definition variable updates).
func registerPolicy(r tools.Registry, m *AdoManager, _ PipelineFormatter) error {
	type toolSpec struct {
		decl    *tools.ToolDeclaration
		handler tools.ToolFunc
	}

	specs := []toolSpec{
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_list_branch_policies",
				Description: "Lists all branch policies (build gates, reviewer requirements, etc.) configured for a specific branch in an Azure DevOps repository.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization": {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":      {Type: "STRING", Description: "The project name or ID."},
						"repository":   {Type: "STRING", Description: "The repository name or ID."},
						"branch_name":  {Type: "STRING", Description: "The branch name (e.g., 'main', 'dev/1.54')."},
					},
					Required: []string{"organization", "project", "repository", "branch_name"},
				},
			},
			handler: m.adoListBranchPolicies,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "ado_update_build_definition_variables",
				Description: "Modifies variables for an existing pipeline (build) definition using a Read-Modify-Write cycle. Useful for locking down variable overrides.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"organization":  {Type: "STRING", Description: "The Azure DevOps organization name."},
						"project":       {Type: "STRING", Description: "The project name or ID."},
						"definition_id": {Type: "INTEGER", Description: "The numeric ID of the build definition to update."},
						"variables":     {Type: "OBJECT", Description: "A map of variable names to objects with 'value', 'isSecret', and 'allowOverride' (CRITICAL for lockdown)."},
					},
					Required: []string{"organization", "project", "definition_id", "variables"},
				},
			},
			handler: func(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
				result, err := m.UpdateBuildDefinitionVariables(ctx, args)
				if err != nil {
					return tools.ToolResult{}, err
				}
				if result.Cancelled {
					return tools.ToolResult{Text: "Update cancelled by user."}, nil
				}
				return tools.ToolResult{Text: fmt.Sprintf("Successfully updated variables for build definition %d", result.DefinitionID)}, nil
			},
		},
	}

	for _, spec := range specs {
		if err := r.RegisterToToolkit("ado", spec.decl, spec.handler); err != nil {
			return err
		}
	}

	return nil
}
