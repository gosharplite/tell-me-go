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

// defPatterns defines regex patterns for detecting definitions in supported languages.
var defPatterns = []string{
	`^def\s+\w+`,            // Python function
	`^class\s+\w+`,          // Python/JS class
	`^function\s+`,          // JS function
	`^const\s+\w+\s*=\s*\(`, // JS arrow function
	`^\w+\(\)\s*\{`,         // Bash function
}

// fileProcessor is a callback function for processing a file during a walk.
type fileProcessor func(filePath string) error

func (m *fileSystemManager) searchFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
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
		return m.scanFile(ctx, filePath, func(line string) bool {
			return re.MatchString(line)
		}, &results)
	}

	err = m.walkAndProcess(ctx, params.Path, processor)
	return m.formatSearchResults(results, err)
}

func (m *fileSystemManager) findFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	if params.Pattern == "" {
		return types.ToolResult{}, fmt.Errorf("pattern argument is required")
	}

	var results []string
	processor := func(filePath string) error {
		matched, err := filepath.Match(params.Pattern, filepath.Base(filePath))
		if err != nil {
			return err
		}
		if matched {
			results = append(results, filePath)
		}
		return nil
	}

	if err := m.walkAndProcess(ctx, params.Path, processor); err != nil {
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

	// 1. AST Search (Go files)
	nav := &navigation.Manager{SP: m.sm}
	results, _ := nav.GrepDefinitionsGo(ctx, resolvedPath, params.Query)

	// 2. Prepare Regex for Fallback
	var reQuery *regexp.Regexp
	if params.Query != "" {
		reQuery, err = regexp.Compile("(?i)" + params.Query)
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("invalid query regex: %w", err)
		}
	}

	// 3. Fallback Walk (Non-Go files)
	processor := func(filePath string) error {
		ext := filepath.Ext(filePath)
		if ext == ".go" || !isSupportedDefExt(ext) {
			return nil
		}

		return m.scanFile(ctx, filePath, func(line string) bool {
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
	if err := m.walkAndProcess(ctx, resolvedPath, processor); err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}
	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

// --- Helper Methods ---

// walkAndProcess handles the generic filesystem traversal, safety checks, and directory filtering.
func (m *fileSystemManager) walkAndProcess(ctx context.Context, path string, fn fileProcessor) error {
	// If path isn't absolute/resolved yet, check safety
	if !filepath.IsAbs(path) {
		if path == "" {
			path = "."
		}
		var err error
		path, err = m.sm.IsPathSafe(path)
		if err != nil {
			return err
		}
	}

	return m.fs.Walk(ctx, path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // Skip items we can't access
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if info.IsDir() {
			if isIgnoredDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		return fn(filePath)
	})
}

// scanFile opens a file, checks for binary content, and scans lines with a matcher function.
func (m *fileSystemManager) scanFile(ctx context.Context, filePath string, matcher func(string) bool, results *[]string) error {
	file, err := m.fs.Open(ctx, filePath)
	if err != nil {
		return nil
	}
	defer file.Close()

	if isBin, err := m.checkBinary(file); err != nil || isBin {
		return nil
	}

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if matcher(line) {
			*results = append(*results, formatMatch(filePath, lineNum, line))
			if len(*results) > 100 {
				return fmt.Errorf("too many results")
			}
		}
	}
	return nil
}

// checkBinary reads the beginning of the file to check for binary content and rewinds the cursor.
func (m *fileSystemManager) checkBinary(file fsutil.File) (bool, error) {
	buf := make([]byte, 1024)
	n, err := file.Read(buf)
	if err != nil && err != io.EOF {
		return false, err
	}
	if _, err := file.Seek(0, 0); err != nil {
		return false, err
	}
	return fsutil.IsBinary(buf[:n]), nil
}

func (m *fileSystemManager) formatSearchResults(results []string, err error) (types.ToolResult, error) {
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

func isIgnoredDir(name string) bool {
	return name == ".git" || name == "node_modules" || name == "vendor"
}

func isSupportedDefExt(ext string) bool {
	return ext == ".py" || ext == ".js" || ext == ".sh" || ext == ".md"
}

func formatMatch(path string, lineNum int, text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 500 {
		trimmed = trimmed[:500] + " (truncated)"
	}
	return fmt.Sprintf("%s:%d: %s", path, lineNum, trimmed)
}
