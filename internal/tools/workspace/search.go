// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

type fileSearcher struct {
	sm *security.SecurityManager
	fs storage.FileSystem
}

// defPatterns defines regex patterns for detecting definitions in supported languages.
var defPatterns = []string{
	`^def\s+\w+`,              // Python function
	`^class\s+\w+`,            // Python/JS class
	`^function\s+`,            // JS function
	`^const\s+\w+\s*=\s*\(`,   // JS arrow function
	`^\w+\(\)\s*\{`,           // Bash function
	`^func\s+`,                // Go function
	`^type\s+\w+\s+struct`,    // Go struct
	`^type\s+\w+\s+interface`, // Go interface
}

func (s *fileSearcher) searchFiles(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Query == "" {
		return tools.ToolResult{}, fmt.Errorf("query argument is required")
	}

	re, err := regexp.Compile(params.Query)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	results, err := ConcurrentSearch(ctx, s.sm, s.fs, params.Path, func(_, line string) bool {
		return re.MatchString(line)
	}, 100)

	return s.formatSearchResults(results, err, "No matches found.")
}

func (s *fileSearcher) grepDefinitions(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	var reQuery *regexp.Regexp
	if params.Query != "" {
		reQuery, _ = regexp.Compile("(?i)" + params.Query)
	}

	matcher := getDefinitionMatcher(reQuery, getCompiledPatterns())
	results, err := ConcurrentSearch(ctx, s.sm, s.fs, path, matcher, 100)

	return s.formatSearchResults(results, err, "No definitions found.")
}

func getCompiledPatterns() []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, len(defPatterns))
	for i, p := range defPatterns {
		compiled[i] = regexp.MustCompile(p)
	}
	return compiled
}

func getDefinitionMatcher(reQuery *regexp.Regexp, compiledDefs []*regexp.Regexp) func(string, string) bool {
	return func(path, line string) bool {
		if !isSupportedDefExt(filepath.Ext(path)) {
			return false
		}

		isDef := false
		for _, re := range compiledDefs {
			if re.MatchString(line) {
				isDef = true
				break
			}
		}

		if !isDef {
			return false
		}

		return reQuery == nil || reQuery.MatchString(line)
	}
}

func (s *fileSearcher) formatSearchResults(results []string, err error, emptyMsg string) (tools.ToolResult, error) {
	if err != nil && err.Error() != "too many results" {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: emptyMsg}, nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return tools.ToolResult{Text: out}, nil
}

func isSupportedDefExt(ext string) bool {
	return ext == ".py" || ext == ".js" || ext == ".sh" || ext == ".md" || ext == ".go"
}
