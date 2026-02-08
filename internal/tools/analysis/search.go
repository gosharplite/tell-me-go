// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package analysis

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

type SearchManager struct {
	SP security.SecurityProvider
	FS fsutil.FileSystem
}

var todoRegex = regexp.MustCompile(`(?i)(TODO|FIXME|BUG):?.*`)

func (m *SearchManager) ListTodos(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	results, err := workspace.ConcurrentSearch(ctx, m.SP, m.FS, path, func(_, line string) bool {
		return todoRegex.MatchString(line)
	}, 500)

	if err != nil && err.Error() != "too many results" {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No TODOs, FIXMEs, or BUGs found."}, nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}

func (m *SearchManager) SearchUsagesGlobally(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
		Path  string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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

	results, err := workspace.ConcurrentSearch(ctx, m.SP, m.FS, path, func(_, line string) bool {
		return re.MatchString(line)
	}, 100)

	if err != nil && err.Error() != "too many results" {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No matches found."}, nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}
