// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type fileWriter struct {
	sm *security.SecurityManager
	bm *BackupManager
	fs fsutil.FileSystem
}

func (w *fileWriter) writeFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		Content  string `json:"content"`
		Reason   string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	content := params.Content

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	detail := fmt.Sprintf("Reason: %s\n\nContent (full):\n%s", params.Reason, content)
	approved, err := w.sm.ConfirmDestructiveAction(ctx, "WRITE FILE", resolvedPath, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	w.bm.Snapshot(resolvedPath, "WRITE")

	// Create parent directories if they don't exist
	dir := filepath.Dir(resolvedPath)
	if err := w.fs.MkdirAll(ctx, dir, 0755); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to create directories: %w", err)
	}

	err = w.fs.WriteFile(ctx, resolvedPath, []byte(content), 0644)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return types.ToolResult{Text: "File written successfully."}, nil
}

func (w *fileWriter) replaceText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		OldText  string `json:"old_text"`
		NewText  string `json:"new_text"`
		Reason   string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	oldText := params.OldText
	newText := params.NewText

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	detail := fmt.Sprintf("Reason: %s\n\nReplace (first occurrence):\n%s\nWith:\n%s", params.Reason, oldText, newText)
	approved, err := w.sm.ConfirmDestructiveAction(ctx, "REPLACE TEXT", resolvedPath, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	w.bm.Snapshot(resolvedPath, "REPLACE")

	contentBytes, err := w.fs.ReadFile(ctx, resolvedPath)
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
	err = w.fs.WriteFile(ctx, resolvedPath, []byte(newContent), 0644)
	if err != nil {
		return types.ToolResult{}, err
	}

	return types.ToolResult{Text: "File updated successfully."}, nil
}

func (w *fileWriter) appendText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		Content  string `json:"content"`
		Reason   string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	content := params.Content

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	detail := fmt.Sprintf("Reason: %s\n\nContent:\n%s", params.Reason, content)
	approved, err := w.sm.ConfirmDestructiveAction(ctx, "APPEND TEXT", resolvedPath, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	w.bm.Snapshot(resolvedPath, "APPEND")

	// Use OpenFile with O_APPEND
	f, err := w.fs.OpenFile(ctx, resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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

func (w *fileWriter) undoFileChange(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		N int `json:"n"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	n := params.N
	if n <= 0 {
		n = 1
	}
	res, err := w.bm.Undo(ctx, n)
	return types.ToolResult{Text: res}, err
}
