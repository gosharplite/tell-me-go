// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/tools/navigation"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func (m *fileSystemManager) searchFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query
	if query == "" {
		return types.ToolResult{}, fmt.Errorf("query argument is required")
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = m.fs.Walk(ctx, resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		file, err := m.fs.Open(ctx, filePath)
		if err != nil {
			return nil
		}
		defer file.Close()

		// Read first 1024 bytes to check if binary
		buf := make([]byte, 1024)
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return nil // Skip file on error
		}
		if fsutil.IsBinary(buf[:n]) {
			return nil
		}
		file.Seek(0, 0)

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				trimmed := strings.TrimSpace(line)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, lineNum, trimmed))
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

func (m *fileSystemManager) findFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	pattern := params.Pattern
	if pattern == "" {
		return types.ToolResult{}, fmt.Errorf("pattern argument is required")
	}

	var results []string
	err = m.fs.Walk(ctx, resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		matched, err := filepath.Match(pattern, info.Name())
		if err != nil {
			return err
		}

		if matched {
			results = append(results, filePath)
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No files found matching pattern."}, nil
	}

	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *fileSystemManager) grepDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	query := params.Query

	// Attempt AST-based search for Go files first
	nav := &navigation.Manager{SP: m.sm}
	astResults, err := nav.GrepDefinitionsGo(ctx, resolvedPath, query)
	if err != nil {
		// Fallback to regex if AST fails for some reason
	}

	// Broad definition patterns for non-Go files
	defPatterns := []string{
		`^def\s+\w+`,            // Python function
		`^class\s+\w+`,          // Python/JS class
		`^function\s+`,          // JS function
		`^const\s+\w+\s*=\s*\(`, // JS arrow function
		`^\w+\(\)\s*\{`,         // Bash function
	}

	var reQuery *regexp.Regexp
	if query != "" {
		var err error
		reQuery, err = regexp.Compile("(?i)" + query)
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("invalid query regex: %w", err)
		}
	}

	var results []string
	results = append(results, astResults...)

	err = m.fs.Walk(ctx, resolvedPath, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only check common source files, skip .go if we already handled them with AST (which we do by default now)
		ext := filepath.Ext(filePath)
		if ext != ".py" && ext != ".js" && ext != ".sh" && ext != ".md" {
			return nil
		}

		file, err := m.fs.Open(ctx, filePath)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			isDef := false
			for _, p := range defPatterns {
				if matched, _ := regexp.MatchString(p, line); matched {
					isDef = true
					break
				}
			}

			if isDef {
				if reQuery == nil || reQuery.MatchString(line) {
					trimmed := strings.TrimSpace(line)
					if len(trimmed) > 500 {
						trimmed = trimmed[:500] + " (truncated)"
					}
					results = append(results, fmt.Sprintf("%s:%d: %s", filePath, lineNum, trimmed))
				}
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}

	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}
