// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ado

import (
	"fmt"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// adoPipelineRunDetail holds the decoded fields for a single pipeline run.
type adoPipelineRunDetail struct {
	Id      int    `json:"id"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Result  string `json:"result"`
	Created string `json:"createdDate"`
	Url     string `json:"url"`
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

func formatPipelineRunsList(pipelineId int, runs []adoPipelineRun) string {
	if len(runs) == 0 {
		return "No pipeline runs found."
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Recent runs for pipeline %d:\n\n", pipelineId)
	for _, run := range runs {
		_, _ = fmt.Fprintf(&resultText, "- Run ID: %d, Name: %s, Status: %s, Result: %s, Created: %s, Repo: %s\n",
			run.Id, run.Name, run.State, run.Result, run.Created, run.Repository.Name)
	}

	return resultText.String()
}

func formatPipelineRunDetail(run *adoPipelineRunDetail) string {
	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Pipeline Run #%d Details:\n", run.Id)
	_, _ = fmt.Fprintf(&resultText, "- Name: %s\n", run.Name)
	_, _ = fmt.Fprintf(&resultText, "- Status: %s\n", run.State)
	_, _ = fmt.Fprintf(&resultText, "- Result: %s\n", run.Result)
	_, _ = fmt.Fprintf(&resultText, "- Created: %s\n", run.Created)
	_, _ = fmt.Fprintf(&resultText, "- URL: %s\n", run.Url)
	return resultText.String()
}

// FormatRepositoryItems formats repository items for display.
func FormatRepositoryItems(scopePath, version string, responseData adoRepositoryItemsResponse) tools.ToolResult {
	if len(responseData.Value) == 0 {
		return tools.ToolResult{Text: "No items found."}
	}

	var resultText strings.Builder
	_, _ = fmt.Fprintf(&resultText, "Items in %s (%s):\n\n", scopePath, version)
	for _, item := range responseData.Value {
		// Skip the scope path itself if it's the first element
		if item.Path == scopePath && len(responseData.Value) > 1 {
			continue
		}
		prefix := "[FILE]"
		if item.IsFolder {
			prefix = "[DIR] "
		}
		_, _ = fmt.Fprintf(&resultText, "- %s %s\n", prefix, item.Path)
	}

	return tools.ToolResult{Text: resultText.String()}
}

// Helper functions to reduce complexity in formatKey
func isLower(r rune) bool {
	return r >= 'a' && r <= 'z'
}

func isUpper(r rune) bool {
	return r >= 'A' && r <= 'Z'
}

func toUpper(r rune) rune {
	if isLower(r) {
		return r - 'a' + 'A'
	}
	return r
}

func formatKey(s string) string {
	if s == "" {
		return ""
	}
	runes := []rune(s)
	var res strings.Builder

	for i, r := range runes {
		if i == 0 {
			res.WriteRune(toUpper(r))
			continue
		}

		if isUpper(r) {
			// Add space if preceded by lowercase OR followed by lowercase (e.g., HTMLReader -> HTML Reader)
			prevLower := isLower(runes[i-1])
			nextLower := i+1 < len(runes) && isLower(runes[i+1])
			if prevLower || nextLower {
				res.WriteRune(' ')
			}
		}
		res.WriteRune(r)
	}

	result := res.String()
	if strings.HasSuffix(result, " Id") {
		result = result[:len(result)-2] + "ID"
	}
	return result
}
