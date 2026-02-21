// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	"github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

type searchManager struct {
	SP security.PathValidator
	FS persistence.FileSystem
}

var todoRegex = regexp.MustCompile(`(?i)(TODO|FIXME|BUG):?.*`)

func (m *searchManager) ListTodos(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan, errChan := workspace.ConcurrentSearch(ctx, m.SP, m.FS, path, func(_, line string) bool {
		return todoRegex.MatchString(line)
	})

	var results []string
	limit := 500
	truncated := false
	for res := range resChan {
		if len(results) >= limit {
			truncated = true
			cancel()
			break
		}
		results = append(results, res)
	}

	var finalErr error
	select {
	case err := <-errChan:
		finalErr = err
	default:
	}
	if finalErr != nil {
		return tools.ToolResult{}, finalErr
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No TODOs, FIXMEs, or BUGs found."}, nil
	}

	out := strings.Join(results, "\n")
	if truncated {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}

func (m *searchManager) SearchUsagesGlobally(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	re, err := regexp.Compile(params.Query)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan, errChan := workspace.ConcurrentSearch(ctx, m.SP, m.FS, path, func(_, line string) bool {
		return re.MatchString(line)
	})

	var results []string
	limit := 100
	truncated := false
	for res := range resChan {
		if len(results) >= limit {
			truncated = true
			cancel()
			break
		}
		results = append(results, res)
	}

	var finalErr error
	select {
	case err := <-errChan:
		finalErr = err
	default:
	}
	if finalErr != nil {
		return tools.ToolResult{}, finalErr
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No matches found."}, nil
	}

	out := strings.Join(results, "\n")
	if truncated {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}
