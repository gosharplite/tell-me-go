// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
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
		Description: "Retrieves the short status of the git repository (staged, unstaged, and untracked files).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
		},
	}, m.getGitStatus)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_git_diff",
		Description: "Retrieves the git diff between the working directory (or staged index) and the last commit. Use this to review changes before committing.",
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

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "git_commit",
		Description: "Commits staged changes with a message.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"message": {
					Type:        genai.TypeString,
					Description: "The commit message.",
				},
			},
			Required: []string{"message"},
		},
	}, m.gitCommit, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "git_create_branch",
		Description: "Creates and checks out a new git branch.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"name": {
					Type:        genai.TypeString,
					Description: "The name of the new branch.",
				},
			},
			Required: []string{"name"},
		},
	}, m.gitCreateBranch, ToolOptions{Serial: true})
}

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	res, err := runGitCommand(ctx, "status", "--short")
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	staged, _ := args["staged"].(bool)
	var res string
	var err error
	if staged {
		res, err = runGitCommand(ctx, "diff", "--staged")
	} else {
		res, err = runGitCommand(ctx, "diff")
	}
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
	}
	res, err := runGitCommand(ctx, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	hash, ok := args["hash"].(string)
	if !ok || hash == "" {
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
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	res, err := runGitCommand(ctx, "blame", "-w", path)
	return types.ToolResult{Text: res}, err
}

func (m *gitManager) gitCommit(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	message, _ := args["message"].(string)
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
	name, _ := args["name"].(string)
	if name == "" {
		return types.ToolResult{}, fmt.Errorf("branch name is required")
	}

	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT CREATE BRANCH", name, "")
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
