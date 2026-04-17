// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type commandExecutor interface {
	RunCommand(ctx context.Context, parts []string, config executionConfig) (executionResult, error)
	LookPath(file string) (string, error)
}

type fileReader struct {
	sm       domain_security.PathValidator
	fs       persistence.FileSystem
	executor commandExecutor
}

func (r *fileReader) listFiles(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
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

func (r *fileReader) getTree(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
		Reason   string `json:"reason"`
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
	err = buildTree(ctx, r.fs, resolvedPath, "", 0, maxDepth, &sb, hb)
	if err != nil {
		return tools.ToolResult{}, err
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func buildTree(ctx context.Context, fs persistence.FileSystem, path, indent string, depth, maxDepth int, sb *strings.Builder, hb chan<- struct{}) error {
	if depth > maxDepth {
		return nil
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if hb != nil {
		sendHeartbeat(ctx, hb)
	}

	entries, err := fs.ReadDir(ctx, path)
	if err != nil {
		return err
	}

	for i, entry := range entries {
		isLast := i == len(entries)-1
		if err := writeTreeEntry(ctx, fs, entry, path, indent, isLast, depth, maxDepth, sb, hb); err != nil {
			return err
		}
	}
	return nil
}

func writeTreeEntry(ctx context.Context, fs persistence.FileSystem, entry os.DirEntry, parentPath, indent string, isLast bool, depth, maxDepth int, sb *strings.Builder, hb chan<- struct{}) error {
	connector := "├── "
	if isLast {
		connector = "└── "
	}

	sb.WriteString(indent + connector + entry.Name() + "\n")

	if !entry.IsDir() || entry.Name() == ".git" {
		return nil
	}

	newIndent := indent + "│   "
	if isLast {
		newIndent = indent + "    "
	}

	return buildTree(ctx, fs, filepath.Join(parentPath, entry.Name()), newIndent, depth+1, maxDepth, sb, hb)
}

const maxReadSize = 100000

func (r *fileReader) readBoundedContent(ctx context.Context, path string) ([]byte, bool, error) {
	f, err := r.fs.Open(ctx, path)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = f.Close() }()

	// Read up to maxReadSize + 1 to detect truncation
	content, err := io.ReadAll(io.LimitReader(f, int64(maxReadSize)+1))
	if err != nil {
		return nil, false, err
	}

	if len(content) > maxReadSize {
		return content[:maxReadSize], true, nil
	}
	return content, false, nil
}

func (r *fileReader) readFile(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		Reason   string `json:"reason"`
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

	info, err := r.fs.Stat(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	if info.IsDir() {
		return tools.ToolResult{}, fmt.Errorf("path is a directory, use list_files instead")
	}

	content, truncated, err := r.readBoundedContent(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	if persistence.IsBinary(content) {
		return tools.ToolResult{Text: fmt.Sprintf("File %s appears to be a binary file and cannot be displayed as text.", resolvedPath)}, nil
	}

	if truncated {
		truncatedStr := string(content)
		return tools.ToolResult{Text: strings.ToValidUTF8(truncatedStr, "") + "\n... (truncated)"}, nil
	}

	return tools.ToolResult{Text: string(content)}, nil
}

func (r *fileReader) processSingleFile(ctx context.Context, path string, sb *strings.Builder) error {
	fmt.Fprintf(sb, "--- File: %s ---\n", path)

	resolvedPath, err := r.sm.IsPathSafe(path)
	if err != nil {
		fmt.Fprintf(sb, "ERROR: %v\n\n", err)
		return nil
	}

	info, err := r.fs.Stat(ctx, resolvedPath)
	if err != nil {
		fmt.Fprintf(sb, "ERROR: failed to read file: %v\n\n", err)
		return nil
	}
	if info.IsDir() {
		sb.WriteString("ERROR: path is a directory, use list_files instead\n\n")
		return nil
	}

	content, truncated, err := r.readBoundedContent(ctx, resolvedPath)
	if err != nil {
		fmt.Fprintf(sb, "ERROR: failed to read file: %v\n\n", err)
		return nil
	}

	if persistence.IsBinary(content) {
		sb.WriteString("(Binary file, cannot display as text)\n\n")
		return nil
	}

	if truncated {
		truncatedStr := string(content)
		sb.WriteString(strings.ToValidUTF8(truncatedStr, ""))
		sb.WriteString("\n... (truncated)\n\n")
		return nil
	}

	sb.Write(content)
	sb.WriteString("\n\n")
	return nil
}

func (r *fileReader) readFiles(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		FilePaths []string `json:"filepaths"`
		Reason    string   `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid arguments: %w", err)
	}

	if len(params.FilePaths) == 0 {
		return tools.ToolResult{}, fmt.Errorf("filepaths argument is required and cannot be empty")
	}

	const maxFilesPerCall = 50
	if len(params.FilePaths) > maxFilesPerCall {
		return tools.ToolResult{}, fmt.Errorf("requested too many files (%d). Maximum allowed per call is %d", len(params.FilePaths), maxFilesPerCall)
	}

	var sb strings.Builder
	for i, path := range params.FilePaths {
		// Emit heartbeat every 5 files
		if i%5 == 0 && hb != nil {
			sendHeartbeat(ctx, hb)
		}
		if err := r.processSingleFile(ctx, path, &sb); err != nil {
			return tools.ToolResult{}, err
		}
	}

	return tools.ToolResult{Text: sb.String()}, nil
}

func (r *fileReader) findFile(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
		Reason  string `json:"reason"`
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

	if err := walkAndProcess(ctx, r.sm, r.fs, params.Path, hb, processor); err != nil {
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

	if r.executor == nil {
		return fmt.Errorf("no command executor available for diffing")
	}

	// Check for diff binary
	if _, err := r.executor.LookPath("diff"); err != nil {
		return fmt.Errorf("'diff' command not found in system PATH: %w", err)
	}

	return nil
}

func (r *fileReader) getFileDiff(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		File1  string `json:"file1"`
		File2  string `json:"file2"`
		Reason string `json:"reason"`
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

	// SECURE EXECUTION: Use the executor for portability and redirection handling
	res, err := r.executor.RunCommand(ctx, []string{"diff", "-u", "--", resolved1, resolved2}, executionConfig{})

	if len(res.Output) == 0 && err == nil {
		return tools.ToolResult{Text: "Files are identical."}, nil
	}

	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("diff failed: %w", err)
	}

	if res.ExitCode != 0 && res.ExitCode != 1 { // 1 is normal for diff finding differences
		return tools.ToolResult{Text: res.Output}, fmt.Errorf("diff process failed with exit code %d", res.ExitCode)
	}

	return tools.ToolResult{Text: res.Output}, nil
}
