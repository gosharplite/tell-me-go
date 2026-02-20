// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type gitManager struct {
	sm   domain_security.ISecurityManager
	Exec tools.CommandExecutor
}

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	res, err := m.runGitCommand(ctx, "status", "--short")
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Staged bool `json:"staged"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	var res string
	var err error
	if params.Staged {
		res, err = m.runGitCommand(ctx, "diff", "--staged")
	} else {
		res, err = m.runGitCommand(ctx, "diff")
	}
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Limit int `json:"limit"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	limit := params.Limit
	if limit <= 0 {
		limit = 10
	}
	res, err := m.runGitCommand(ctx, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Hash string `json:"hash"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	hash := params.Hash
	if hash == "" {
		return tools.ToolResult{}, fmt.Errorf("hash argument is required")
	}
	// Truncate output to prevent hitting token limits on very large diffs
	out, err := m.runGitCommand(ctx, "show", "--stat", "--patch", hash)
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
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	res, err := m.runGitCommand(ctx, "blame", "-w", resolvedPath)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) gitCommit(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	message := params.Message
	if message == "" {
		return tools.ToolResult{}, fmt.Errorf("message is required")
	}

	detail := fmt.Sprintf("Reason: %s\nMessage: %s", params.Reason, message)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "GIT COMMIT", "current staged changes", detail)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !approved {
		return tools.ToolResult{Text: "Action denied by user."}, nil
	}

	res, err := m.runGitCommand(ctx, "commit", "-m", message)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) gitCreateBranch(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	res, err := m.runGitCommand(ctx, "checkout", "-b", name)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) runGitCommand(ctx context.Context, args ...string) (string, error) {
	out, err := m.Exec.CombinedOutput(ctx, "git", args...)
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}
