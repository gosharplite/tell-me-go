// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type fileReader struct {
	sm *security.SecurityManager
	fs fsutil.FileSystem
}

func (r *fileReader) listFiles(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	entries, err := r.fs.ReadDir(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to list directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contents of %s:\n", resolvedPath))
	for _, entry := range entries {
		typeStr := "f"
		if entry.IsDir() {
			typeStr = "d"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s\n", typeStr, entry.Name()))
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (r *fileReader) getTree(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		path = "."
	}

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	maxDepth := params.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}

	var sb strings.Builder
	err = buildTree(ctx, r.fs, resolvedPath, "", 0, maxDepth, &sb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func buildTree(ctx context.Context, fs fsutil.FileSystem, path, indent string, depth, maxDepth int, sb *strings.Builder) error {
	if depth > maxDepth {
		return nil
	}
	entries, err := fs.ReadDir(ctx, path)
	if err != nil {
		return err
	}

	for i, entry := range entries {
		connector := "├── "
		if i == len(entries)-1 {
			connector = "└── "
		}

		sb.WriteString(indent + connector + entry.Name() + "\n")

		if entry.IsDir() {
			newIndent := indent + "│   "
			if i == len(entries)-1 {
				newIndent = indent + "    "
			}
			// Skip .git
			if entry.Name() == ".git" {
				continue
			}
			buildTree(ctx, fs, filepath.Join(path, entry.Name()), newIndent, depth+1, maxDepth, sb)
		}
	}
	return nil
}

func (r *fileReader) readFile(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.FilePath
	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	content, err := r.fs.ReadFile(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) > 100000 {
		return tools.ToolResult{Text: string(content[:100000]) + "\n... (truncated)"}, nil
	}

	return tools.ToolResult{Text: string(content)}, nil
}

func (r *fileReader) findFile(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Pattern == "" {
		return tools.ToolResult{}, fmt.Errorf("pattern argument is required")
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

	// fileReader doesn't have walkAndProcess, I should probably move it to a shared place or redefine it.
	// Actually, search_files uses walkAndProcess. I'll move walkAndProcess to a internal helper in this package.
	if err := walkAndProcess(ctx, r.sm, r.fs, params.Path, processor); err != nil {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No files found matching pattern."}, nil
	}
	return tools.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (r *fileReader) getFileDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		File1 string `json:"file1"`
		File2 string `json:"file2"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	file1 := params.File1
	file2 := params.File2

	resolved1, err := r.sm.IsPathSafe(file1)
	if err != nil {
		return tools.ToolResult{}, err
	}
	resolved2, err := r.sm.IsPathSafe(file2)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if _, err := exec.LookPath("diff"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("'diff' command not found: %w", err)
	}

	cmd := exec.CommandContext(ctx, "diff", "-u", resolved1, resolved2)
	out, err := cmd.CombinedOutput()

	if len(out) == 0 && err == nil {
		return tools.ToolResult{Text: "Files are identical."}, nil
	}

	// diff returns exit code 1 if files differ, which is an error for Command.CombinedOutput
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return tools.ToolResult{}, fmt.Errorf("diff failed: %w", err)
		}
	}

	return tools.ToolResult{Text: string(out)}, nil
}
