// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

func (m *fileSystemManager) replaceText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		OldText  string `json:"old_text"`
		NewText  string `json:"new_text"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	oldText := params.OldText
	newText := params.NewText

	resolvedPath, err := m.sm.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	detail := fmt.Sprintf("Replace (first occurrence):\n%s\nWith:\n%s", oldText, newText)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "REPLACE TEXT", resolvedPath, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	m.bm.Snapshot(resolvedPath, "REPLACE")

	contentBytes, err := m.fs.ReadFile(ctx, resolvedPath)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(contentBytes)

	count := strings.Count(content, oldText)
	if count == 0 {
		return types.ToolResult{}, fmt.Errorf("old_text not found in file")
	}
	if count > 1 {
		return types.ToolResult{}, fmt.Errorf("old_text found %d times. Please provide more context (including surrounding lines) to uniquely identify the replacement target", count)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	err = m.fs.WriteFile(ctx, resolvedPath, []byte(newContent), 0644)
	if err != nil {
		return types.ToolResult{}, err
	}

	return types.ToolResult{Text: "File updated successfully."}, nil
}

func (m *fileSystemManager) getFileDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		File1 string `json:"file1"`
		File2 string `json:"file2"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	file1 := params.File1
	file2 := params.File2

	resolved1, err := m.sm.IsPathSafe(file1)
	if err != nil {
		return types.ToolResult{}, err
	}
	resolved2, err := m.sm.IsPathSafe(file2)
	if err != nil {
		return types.ToolResult{}, err
	}

	cmd := exec.CommandContext(ctx, "diff", "-u", resolved1, resolved2)
	out, _ := cmd.CombinedOutput()

	if len(out) == 0 {
		return types.ToolResult{Text: "Files are identical."}, nil
	}

	return types.ToolResult{Text: string(out)}, nil
}

func (m *fileSystemManager) undoFileChange(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		N int `json:"n"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	n := params.N
	if n <= 0 {
		n = 1
	}
	res, err := m.bm.Undo(ctx, n)
	return types.ToolResult{Text: res}, err
}

func (m *fileSystemManager) appendText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		Content  string `json:"content"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	content := params.Content

	resolvedPath, err := m.sm.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "APPEND TEXT", resolvedPath, content)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	m.bm.Snapshot(resolvedPath, "APPEND")

	// Use OpenFile with O_APPEND
	f, err := m.fs.OpenFile(ctx, resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	if _, err := f.Write([]byte(content)); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to append: %w", err)
	}

	if err := f.Close(); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to close file (data may be lost): %w", err)
	}

	return types.ToolResult{Text: "Text appended successfully."}, nil
}
