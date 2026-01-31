// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package code

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type SearchManager struct {
	SP types.SecurityProvider
}

func (m *SearchManager) ListTodos(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.SP.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	re := regexp.MustCompile(`(?i)(TODO|FIXME|BUG):?.*`)
	var results []string

	err = filepath.Walk(resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				match := re.FindString(line)
				trimmed := strings.TrimSpace(match)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, i+1, trimmed))
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}
	if len(results) == 0 {
		return types.ToolResult{Text: "No TODOs, FIXMEs, or BUGs found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *SearchManager) SearchUsagesGlobally(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Query string `json:"query"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query
	re, err := regexp.Compile(query)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "vendor" || info.Name() == "node_modules" || info.Name() == "output" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip binary files heuristic
		if info.Size() > 1024*1024 { // Skip files > 1MB
			return nil
		}

		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		// Read first 512 bytes for binary check
		head := make([]byte, 512)
		n, _ := f.Read(head)
		if fsutil.IsBinary(head[:n]) {
			return nil
		}

		// Read full content
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				trimmed := strings.TrimSpace(line)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", path, i+1, trimmed))
				if len(results) > 100 {
					return fmt.Errorf("too many results")
				}
			}
		}
		return nil
	})

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
