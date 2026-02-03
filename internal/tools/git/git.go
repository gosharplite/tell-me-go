// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type gitManager struct {
	sm *security.SecurityManager
}

// Register adds Git-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	m := &gitManager{sm: sm}

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the short status of the git repository (staged, unstaged, and untracked files).",
	}, m.getGitStatus)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_diff",
		Description: "Retrieves the git diff between the working directory (or staged index) and the last commit. Use this to review changes before committing.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"staged": {
					Type:        "BOOLEAN",
					Description: "If true, shows staged changes.",
				},
			},
		},
	}, m.getGitDiff)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_log",
		Description: "Retrieves the git commit log.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"limit": {
					Type:        "INTEGER",
					Description: "Number of commits to show (default: 10).",
				},
			},
		},
	}, m.getGitLog)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_show",
		Description: "Shows the full details (diff and metadata) of a specific commit hash (runs git show).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"hash": {
					Type:        "STRING",
					Description: "The commit hash to inspect.",
				},
			},
			Required: []string{"hash"},
		},
	}, m.getGitCommit)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_git_blame",
		Description: "Shows who changed which lines in a file.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.getGitBlame)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "git_commit",
		Description: "Commits staged changes with a message.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"message": {
					Type:        "STRING",
					Description: "The commit message.",
				},
			},
			Required: []string{"message"},
		},
	}, m.gitCommit, registry.ToolOptions{Serial: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "git_create_branch",
		Description: "Creates and checks out a new git branch.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"name": {
					Type:        "STRING",
					Description: "The name of the new branch.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for creating this branch.",
				},
			},
			Required: []string{"name", "reason"},
		},
	}, m.gitCreateBranch, registry.ToolOptions{Serial: true})
}

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	res, err := runGitCommand(ctx, "status", "--short")
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Staged bool `json:"staged"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	var res string
	var err error
	if params.Staged {
		res, err = runGitCommand(ctx, "diff", "--staged")
	} else {
		res, err = runGitCommand(ctx, "diff")
	}
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Limit int `json:"limit"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	res, err := runGitCommand(ctx, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Hash string `json:"hash"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	hash := params.Hash
	if hash == "" {
		return tools.ToolResult{}, fmt.Errorf("hash argument is required")
	}
	// Truncate output to prevent hitting token limits on very large diffs
	out, err := runGitCommand(ctx, "show", "--stat", "--patch", hash)
	if err != nil {
		return tools.ToolResult{Text: out}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 300 {
		return tools.ToolResult{Text: strings.Join(lines[:300], "\n") + "\n... (Output truncated) ..."}, nil
	}
	return tools.ToolResult{Text: out}, nil
}

func (m *gitManager) getGitBlame(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.FilePath
	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	res, err := runGitCommand(ctx, "blame", "-w", resolvedPath)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) gitCommit(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	message := params.Message
	if message == "" {
		return tools.ToolResult{}, fmt.Errorf("message is required")
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT COMMIT", "current staged changes", message)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, nil
	}

	res, err := runGitCommand(ctx, "commit", "-m", message)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) gitCreateBranch(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	name := params.Name
	if name == "" {
		return tools.ToolResult{}, fmt.Errorf("branch name is required")
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT CREATE BRANCH", name, params.Reason)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, nil
	}

	res, err := runGitCommand(ctx, "checkout", "-b", name)
	return tools.ToolResult{Text: res}, err
}

func runGitCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
