// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type fileReader struct {
	sm domain_security.PathValidator
	fs persistence.FileSystem
}

func (r *fileReader) listFiles(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	entries, err := r.fs.ReadDir(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to list directory: %w", err)
	}

	var sb strings.Builder
	_, _ = fmt.Fprintf(&sb, "Contents of %s:\n", resolvedPath)
	for _, entry := range entries {
		typeStr := "f"
		if entry.IsDir() {
			typeStr = "d"
		}
		_, _ = fmt.Fprintf(&sb, "[%s] %s\n", typeStr, entry.Name())
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (r *fileReader) getTree(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

func buildTree(ctx context.Context, fs persistence.FileSystem, path, indent string, depth, maxDepth int, sb *strings.Builder) error {
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
			if err := buildTree(ctx, fs, filepath.Join(path, entry.Name()), newIndent, depth+1, maxDepth, sb); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *fileReader) readFile(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	content, err := r.fs.ReadFile(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	if persistence.IsBinary(content) {
		return tools.ToolResult{Text: fmt.Sprintf("File %s appears to be a binary file and cannot be displayed as text.", resolvedPath)}, nil
	}

	if len(content) > 100000 {
		truncated := string(content[:100000])
		return tools.ToolResult{Text: strings.ToValidUTF8(truncated, "") + "\n... (truncated)"}, nil
	}

	return tools.ToolResult{Text: string(content)}, nil
}

func (r *fileReader) readFiles(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		FilePaths []string `json:"filepaths"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if len(params.FilePaths) == 0 {
		return tools.ToolResult{}, fmt.Errorf("filepaths argument is required and cannot be empty")
	}

	var sb strings.Builder
	for _, path := range params.FilePaths {
		sb.WriteString(fmt.Sprintf("--- File: %s ---\n", path))

		resolvedPath, err := r.sm.IsPathSafe(path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("ERROR: %v\n\n", err))
			continue
		}

		content, err := r.fs.ReadFile(ctx, resolvedPath)
		if err != nil {
			sb.WriteString(fmt.Sprintf("ERROR: failed to read file: %v\n\n", err))
			continue
		}

		if persistence.IsBinary(content) {
			sb.WriteString(fmt.Sprintf("(Binary file, cannot display as text)\n\n"))
			continue
		}

		if len(content) > 100000 {
			truncated := string(content[:100000])
			sb.WriteString(strings.ToValidUTF8(truncated, ""))
			sb.WriteString("\n... (truncated)\n\n")
			continue
		}

		sb.Write(content)
		sb.WriteString("\n\n")
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (r *fileReader) findFile(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	if err := walkAndProcess(ctx, r.sm, r.fs, params.Path, processor); err != nil {
		return tools.ToolResult{}, err
	}

	if len(results) == 0 {
		return tools.ToolResult{Text: "No files found matching pattern."}, nil
	}
	return tools.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (r *fileReader) validateDiffPrerequisites(ctx context.Context, resolved1, resolved2 string) error {
	if _, err := r.fs.Stat(ctx, resolved1); err != nil {
		return fmt.Errorf("file1 (%q) not found: %w", resolved1, err)
	}
	if _, err := r.fs.Stat(ctx, resolved2); err != nil {
		return fmt.Errorf("file2 (%q) not found: %w", resolved2, err)
	}

	// Check for diff binary (ideally cached at struct initialization, but kept here for now)
	if _, err := exec.LookPath("diff"); err != nil {
		return fmt.Errorf("'diff' command not found in system PATH: %w", err)
	}

	return nil
}

func (r *fileReader) getFileDiff(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	var params struct {
		File1 string `json:"file1"`
		File2 string `json:"file2"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
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

	if err := r.validateDiffPrerequisites(ctx, resolved1, resolved2); err != nil {
		return tools.ToolResult{}, err
	}

	// SECURE EXECUTION: Use '--' to prevent argument injection
	cmd := exec.CommandContext(ctx, "diff", "-u", "--", resolved1, resolved2)
	out, err := cmd.CombinedOutput()

	if len(out) == 0 && err == nil {
		return tools.ToolResult{Text: "Files are identical."}, nil
	}

	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			return tools.ToolResult{}, fmt.Errorf("diff failed: %w", err)
		}
	}

	return tools.ToolResult{Text: string(out)}, nil
}
