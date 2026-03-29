// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"strings"
	"time"

	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type gitManager struct {
	sm   gitSecurity
	Exec tools.CommandExecutor
}

func (m *gitManager) getGitStatus(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	res, err := m.runGitCommand(ctx, hb, "status", "--short")
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Staged bool `json:"staged"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	var res string
	var err error
	if params.Staged {
		res, err = m.runGitCommand(ctx, hb, "diff", "--staged")
	} else {
		res, err = m.runGitCommand(ctx, hb, "diff")
	}
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitLog(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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
	res, err := m.runGitCommand(ctx, hb, "log", "--oneline", "-n", fmt.Sprintf("%d", limit))
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) getGitCommit(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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
	out, err := m.runGitCommand(ctx, hb, "show", "--stat", "--patch", hash)
	if err != nil {
		return tools.ToolResult{Text: out}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) > 300 {
		return tools.ToolResult{Text: strings.Join(lines[:300], "\n") + "\n... (Output truncated) ..."}, nil
	}
	return tools.ToolResult{Text: out}, nil
}

func (m *gitManager) getGitBlame(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	res, err := m.runGitCommand(ctx, hb, "blame", "-w", resolvedPath)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) gitCommit(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	res, err := m.runGitCommand(ctx, hb, "commit", "-m", message)
	if err != nil {
		// If git failed and the output indicates no staged changes, return an actionable error
		if strings.Contains(res, "nothing to commit") || strings.Contains(res, "no changes added to commit") {
			return tools.ToolResult{Text: res}, fmt.Errorf("no staged changes. You must stage files first (e.g., using execute_command with 'git add .') before committing")
		}
		// Otherwise, return the generic error with the raw output
		return tools.ToolResult{Text: res}, err
	}
	return tools.ToolResult{Text: res}, nil
}

func (m *gitManager) gitCreateBranch(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	res, err := m.runGitCommand(ctx, hb, "checkout", "-b", name)
	return tools.ToolResult{Text: res}, err
}

func (m *gitManager) runGitCommand(ctx context.Context, hb chan<- struct{}, args ...string) (string, error) {
	// Heartbeat while git is running
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if hb != nil {
					select {
					case hb <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	defer close(done)

	out, err := m.Exec.CombinedOutput(ctx, "git", args...)
	if err != nil {
		return string(out), fmt.Errorf("git command failed: %w", err)
	}
	return string(out), nil
}

type gitSecurity interface {
	domain_security.PathValidator
}
