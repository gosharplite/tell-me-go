// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"google.golang.org/genai"
)

type gitManager struct {
	sm *SecurityManager
}

// RegisterGitTools adds Git-related tools to the registry.
func RegisterGitTools(r *Registry, sm *SecurityManager) {
	m := &gitManager{sm: sm}

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the current status of the git repository.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, m.getGitStatus)

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
	}, m.getGitDiff)

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
	}, m.getGitLog)

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
	}, m.getGitCommit)

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
	}, m.getGitBlame)
}

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}) (string, error) {
	return runGitCommand(ctx, "status", "--short")
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}) (string, error) {
	staged, _ := args["staged"].(bool)
	if staged {
		return runGitCommand(ctx, "diff", "--staged")
	}
	return runGitCommand(ctx, "diff")
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}) (string, error) {
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	return runGitCommand(ctx, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}) (string, error) {
	hash, ok := args["hash"].(string)
	if !ok || hash == "" {
		return "", fmt.Errorf("hash argument is required")
	}
	// Truncate output to prevent hitting token limits on very large diffs
	out, err := runGitCommand(ctx, "show", "--stat", "--patch", hash)
	if err != nil {
		return out, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 300 {
		return strings.Join(lines[:300], "\n") + "\n... (Output truncated) ...", nil
	}
	return out, nil
}

func (m *gitManager) getGitBlame(ctx context.Context, args map[string]interface{}) (string, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filepath argument is required")
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return "", err
	}

	return runGitCommand(ctx, "blame", "-w", path)
}

func runGitCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
