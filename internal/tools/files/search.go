// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type fileSearcher struct {
	sm *security.SecurityManager
	fs fsutil.FileSystem
}

// defPatterns defines regex patterns for detecting definitions in supported languages.
var defPatterns = []string{
	`^def\s+\w+`,            // Python function
	`^class\s+\w+`,          // Python/JS class
	`^function\s+`,          // JS function
	`^const\s+\w+\s*=\s*\(`, // JS arrow function
	`^\w+\(\)\s*\{`,         // Bash function
}

func (s *fileSearcher) searchFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	if params.Query == "" {
		return types.ToolResult{}, fmt.Errorf("query argument is required")
	}

	re, err := regexp.Compile(params.Query)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	processor := func(filePath string) error {
		return scanFile(ctx, s.fs, filePath, func(line string) bool {
			return re.MatchString(line)
		}, &results)
	}

	err = walkAndProcess(ctx, s.sm, s.fs, params.Path, processor)
	return s.formatSearchResults(results, err)
}

func (s *fileSearcher) grepDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}
	resolvedPath, err := s.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	var results []string

	// Prepare Regex for Fallback
	var reQuery *regexp.Regexp
	if params.Query != "" {
		reQuery, _ = regexp.Compile("(?i)" + params.Query)
	}

	// Fallback Walk (Non-Go files)
	processor := func(filePath string) error {
		ext := filepath.Ext(filePath)
		if !isSupportedDefExt(ext) {
			return nil
		}

		return scanFile(ctx, s.fs, filePath, func(line string) bool {
			isDef := false
			for _, p := range defPatterns {
				if matched, _ := regexp.MatchString(p, line); matched {
					isDef = true
					break
				}
			}
			if !isDef {
				return false
			}
			return reQuery == nil || reQuery.MatchString(line)
		}, &results)
	}

	// We use resolvedPath here since we already checked safety
	if err := walkAndProcess(ctx, s.sm, s.fs, resolvedPath, processor); err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (s *fileSearcher) formatSearchResults(results []string, err error) (types.ToolResult, error) {
	if err != nil && err.Error() != "too many results" {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No matches found."}, nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return types.ToolResult{Text: out}, nil
}

func isSupportedDefExt(ext string) bool {
	return ext == ".py" || ext == ".js" || ext == ".sh" || ext == ".md" || ext == ".go"
}
