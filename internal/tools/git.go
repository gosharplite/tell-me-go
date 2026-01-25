// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"fmt"
	"os/exec"

	"google.golang.org/genai"
)

// RegisterGitTools adds Git-related tools to the registry.
func RegisterGitTools(r *Registry) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the current status of the git repository.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, getGitStatus)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_diff",
		Description: "Retrieves the git diff of the current repository.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"staged": {
					Type:        genai.TypeBoolean,
					Description: "If true, shows staged changes.",
				},
			},
		},
	}, getGitDiff)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_log",
		Description: "Retrieves the git commit log.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"limit": {
					Type:        genai.TypeInteger,
					Description: "Number of commits to show (default: 10).",
				},
			},
		},
	}, getGitLog)
}

func getGitStatus(args map[string]interface{}) (string, error) {
	return runGitCommand("status", "--short")
}

func getGitDiff(args map[string]interface{}) (string, error) {
	staged, _ := args["staged"].(bool)
	if staged {
		return runGitCommand("diff", "--staged")
	}
	return runGitCommand("diff")
}

func getGitLog(args map[string]interface{}) (string, error) {
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	return runGitCommand("log", "--oneline", "-n", fmt.Sprintf("%d", limit))
}

func runGitCommand(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
