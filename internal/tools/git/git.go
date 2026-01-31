// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type gitManager struct {
	sm *security.SecurityManager
}

// Register adds Git-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	m := &gitManager{sm: sm}

	r.Register(&types.ToolDeclaration{
		Name:        "get_git_status",
		Description: "Retrieves the short status of the git repository (staged, unstaged, and untracked files).",
	}, m.getGitStatus)

	r.Register(&types.ToolDeclaration{
		Name:        "get_git_diff",
		Description: "Retrieves the git diff between the working directory (or staged index) and the last commit. Use this to review changes before committing.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"staged": {
					Type:        "BOOLEAN",
					Description: "If true, shows staged changes.",
				},
			},
		},
	}, m.getGitDiff)

	r.Register(&types.ToolDeclaration{
		Name:        "get_git_log",
		Description: "Retrieves the git commit log.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"limit": {
					Type:        "INTEGER",
					Description: "Number of commits to show (default: 10).",
				},
			},
		},
	}, m.getGitLog)

	r.Register(&types.ToolDeclaration{
		Name:        "get_git_commit",
		Description: "Shows the full details (diff and metadata) of a specific commit hash.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"hash": {
					Type:        "STRING",
					Description: "The commit hash to inspect.",
				},
			},
			Required: []string{"hash"},
		},
	}, m.getGitCommit)

	r.Register(&types.ToolDeclaration{
		Name:        "get_git_blame",
		Description: "Shows who changed which lines in a file.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.getGitBlame)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "git_commit",
		Description: "Commits staged changes with a message.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"message": {
					Type:        "STRING",
					Description: "The commit message.",
				},
			},
			Required: []string{"message"},
		},
	}, m.gitCommit, registry.ToolOptions{Serial: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "git_create_branch",
		Description: "Creates and checks out a new git branch.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
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

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	res, err := runGitCommand(ctx, "status", "--short")
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Staged bool `json:"staged"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	var res string
	var err error
	if params.Staged {
		res, err = runGitCommand(ctx, "diff", "--staged")
	} else {
		res, err = runGitCommand(ctx, "diff")
	}
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Limit int `json:"limit"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	res, err := runGitCommand(ctx, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Hash string `json:"hash"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	hash := params.Hash
	if hash == "" {
		return types.ToolResult{}, fmt.Errorf("hash argument is required")
	}
	// Truncate output to prevent hitting token limits on very large diffs
	out, err := runGitCommand(ctx, "show", "--stat", "--patch", hash)
	if err != nil {
		return types.ToolResult{Text: out}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 300 {
		return types.ToolResult{Text: strings.Join(lines[:300], "\n") + "\n... (Output truncated) ..."}, nil
	}
	return types.ToolResult{Text: out}, nil
}

func (m *gitManager) getGitBlame(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	if path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	res, err := runGitCommand(ctx, "blame", "-w", resolvedPath)
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) gitCommit(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Message string `json:"message"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	message := params.Message
	if message == "" {
		return types.ToolResult{}, fmt.Errorf("message is required")
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT COMMIT", "current staged changes", message)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	res, err := runGitCommand(ctx, "commit", "-m", message)
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) gitCreateBranch(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	name := params.Name
	if name == "" {
		return types.ToolResult{}, fmt.Errorf("branch name is required")
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT CREATE BRANCH", name, params.Reason)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	res, err := runGitCommand(ctx, "checkout", "-b", name)
	return types.ToolResult{Text: res}, err
}

func runGitCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
