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
	"github.com/gosharplite/tell-me-go/internal/domain/services"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/tools/workspace"
)

type searchManager struct {
	SP     security.PathValidator
	FS     persistence.FileSystem
	Policy services.WorkspacePolicy
}

var todoRegex = regexp.MustCompile(`(?i)(TODO|FIXME|BUG):?.*`)

func (m *searchManager) executeSearch(ctx context.Context, path string, hb chan<- struct{}, limit int, emptyMsg string, matchFunc func(string, string) (string, bool)) (tools.ToolResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	resChan, errChan := workspace.ConcurrentSearch(ctx, m.SP, m.FS, path, hb, matchFunc, m.Policy)

	var results []string
	truncated := false
	for res := range resChan {
		if len(results) >= limit {
			truncated = true
			cancel()
			break
		}
		results = append(results, res)
	}

	// Blocking receive on errChan is safe: ConcurrentSearch guarantees
	// errChan is closed before resultsChan, so by the time the results
	// loop exits, errChan is either closed (empty) or contains an error.
	finalErr := <-errChan
	if finalErr != nil {
		return tools.ToolResult{}, finalErr
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: emptyMsg}, nil
	}

	out := strings.Join(results, "\n")
	if truncated {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}

func (m *searchManager) ListTodos(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
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

	return m.executeSearch(ctx, path, hb, 500, "No TODOs, FIXMEs, or BUGs found.", func(_, line string) (string, bool) {
		match := todoRegex.FindString(line)
		if match == "" {
			return "", false
		}
		// Clean up common comment patterns
		match = strings.TrimSuffix(match, "*/")
		return strings.TrimSpace(match), true
	})
}

func (m *searchManager) SearchUsagesGlobally(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Query   string `json:"query"`
		Path    string `json:"path"`
		IsRegex bool   `json:"is_regex"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	var matcher func(string, string) (string, bool)
	if params.IsRegex {
		re, err := regexp.Compile(params.Query)
		if err != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid regex: %w (if you intended a literal text search, set 'is_regex' to false)", err)
		}
		matcher = func(_, line string) (string, bool) {
			return "", re.MatchString(line)
		}
	} else {
		matcher = func(_, line string) (string, bool) {
			return "", strings.Contains(line, params.Query)
		}
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	return m.executeSearch(ctx, path, hb, 100, "No matches found.", matcher)
}
