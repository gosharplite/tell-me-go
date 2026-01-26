// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"fmt"
	"os/exec"
	"strings"

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

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_commit",
		Description: "Shows the full details (diff and metadata) of a specific commit hash.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"hash": {
					Type:        genai.TypeString,
					Description: "The commit hash to inspect.",
				},
			},
			Required: []string{"hash"},
		},
	}, getGitCommit)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_blame",
		Description: "Shows who changed which lines in a file.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, getGitBlame)
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

func getGitCommit(args map[string]interface{}) (string, error) {
	hash, ok := args["hash"].(string)
	if !ok || hash == "" {
		return "", fmt.Errorf("hash argument is required")
	}
	// Truncate output to prevent hitting token limits on very large diffs
	out, err := runGitCommand("show", "--stat", "--patch", hash)
	if err != nil {
		return out, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 300 {
		return strings.Join(lines[:300], "\n") + "\n... (Output truncated) ...", nil
	}
	return out, nil
}

func getGitBlame(args map[string]interface{}) (string, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filepath argument is required")
	}

	if err := IsPathSafe(path); err != nil {
		return "", err
	}

	return runGitCommand("blame", "-w", path)
}

func runGitCommand(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
