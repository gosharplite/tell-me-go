// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/persistence"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type fileWriter struct {
	sm writerSecurity
	bm *backupManager
	fs persistence.FileSystem
}

func (w *fileWriter) writeFile(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	// Defense-in-depth presence guard. As of the registry-level
	// validateRequiredArgs check (see internal/infrastructure/registry/
	// registry.go), every call routed through registry.Execute already
	// has its required parameters validated before reaching this
	// handler — so in production this guard is redundant. We keep it
	// for two reasons:
	//   (a) Direct unit tests in writer_test.go bypass the registry
	//       and call writeFile() directly; this guard preserves their
	//       value as a contract on the handler itself.
	//   (b) Future callers (other registries, embedding scenarios,
	//       in-process tool invocation by tests/scripts) get the same
	//       safety without depending on the registry's behavior.
	// The two layers cannot disagree because both check key-presence
	// (not value-zero-ness) on the same args map.
	if _, ok := args["content"]; !ok {
		return tools.ToolResult{}, fmt.Errorf("required parameter 'content' is missing (to write an empty file, pass content=\"\" explicitly)")
	}

	var params struct {
		FilePath string `json:"filepath"`
		Content  string `json:"content"`
		Reason   string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.FilePath
	content := params.Content

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Confirmation Gate

	w.bm.snapshot(ctx, resolvedPath, "WRITE")

	// Create parent directories if they don't exist
	dir := filepath.Dir(resolvedPath)
	if err := w.fs.MkdirAll(ctx, dir, 0755); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create directories: %w", err)
	}

	err = w.fs.WriteFile(ctx, resolvedPath, []byte(content), 0644)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return tools.ToolResult{Text: "File written successfully."}, nil
}

func (w *fileWriter) replaceText(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		OldText  string `json:"old_text"`
		NewText  string `json:"new_text"`
		Reason   string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.FilePath
	oldText := params.OldText
	newText := params.NewText

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Confirmation Gate

	w.bm.snapshot(ctx, resolvedPath, "REPLACE")

	contentBytes, err := w.fs.ReadFile(ctx, resolvedPath)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}
	content := string(contentBytes)

	count := strings.Count(content, oldText)
	if count == 0 {
		return tools.ToolResult{}, fmt.Errorf("old_text not found in file")
	}
	if count > 1 {
		return tools.ToolResult{}, fmt.Errorf("old_text found %d times. Please provide more context (including surrounding lines) to uniquely identify the replacement target", count)
	}

	newContent := strings.Replace(content, oldText, newText, 1)
	err = w.fs.WriteFile(ctx, resolvedPath, []byte(newContent), 0644)
	if err != nil {
		return tools.ToolResult{}, err
	}

	return tools.ToolResult{Text: "File updated successfully."}, nil
}

func (w *fileWriter) appendText(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (res tools.ToolResult, err error) {
	// Same defense-in-depth pattern as writeFile — see that function's
	// comment. Production calls via registry.Execute hit the central
	// validateRequiredArgs check first; this guard exists for direct
	// invocation paths (tests, scripts, embedding).
	if _, ok := args["content"]; !ok {
		return tools.ToolResult{}, fmt.Errorf("required parameter 'content' is missing (to append nothing, pass content=\"\" explicitly)")
	}

	var params struct {
		FilePath string `json:"filepath"`
		Content  string `json:"content"`
		Reason   string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.FilePath
	content := params.Content

	resolvedPath, err := w.sm.IsPathWritable(path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	// Confirmation Gate

	w.bm.snapshot(ctx, resolvedPath, "APPEND")

	// Use OpenFile with O_APPEND
	f, oerr := w.fs.OpenFile(ctx, resolvedPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if oerr != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to open file: %w", oerr)
	}
	defer func() {
		_ = f.Sync()
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("failed to close file (data may be lost): %w", cerr)
		}
	}()

	if _, err = f.Write([]byte(content)); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to append: %w", err)
	}

	return tools.ToolResult{Text: "Text appended successfully."}, nil
}

func (w *fileWriter) undoFileChange(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		N      int    `json:"n"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	n := params.N
	if n <= 0 {
		return tools.ToolResult{}, fmt.Errorf("n must be positive")
	}

	res, err := w.bm.undo(ctx, n)
	if err == nil && strings.Contains(res, "No snapshots available") {
		return tools.ToolResult{Text: res}, fmt.Errorf("no history found")
	}

	return tools.ToolResult{Text: res}, err
}

type writerSecurity interface {
	domain_security.PathValidator
	domain_security.ActionConfirmer
}

func (w *fileWriter) deletePath(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path      string `json:"path"`
		Recursive bool   `json:"recursive"`
		Reason    string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := w.sm.IsPathWritable(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if params.Recursive {
		return w.performRecursiveDelete(ctx, resolvedPath, params.Reason)
	}

	return w.performSingleDelete(ctx, resolvedPath)
}

func (w *fileWriter) performRecursiveDelete(ctx context.Context, path, reason string) (tools.ToolResult, error) {
	// 1. Mandatory Confirmation for Dangerous Operation
	detail := fmt.Sprintf("Deleting directory and ALL its contents: %s", path)
	approved, err := w.sm.Authorize(ctx, "RECURSIVE_DELETE", detail, reason, false)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("authorization failed: %w", err)
	}
	if !approved {
		return tools.ToolResult{}, fmt.Errorf("recursive deletion not authorized by user")
	}

	// 2. Perform Deletion
	if err := w.fs.RemoveAll(ctx, path); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to delete path recursively: %w", err)
	}

	// 3. Return with explicit warning
	return tools.ToolResult{
		Text: "Path deleted successfully. NOTE: Recursive deletions are irreversible and cannot be undone via undo_file_change.",
	}, nil
}

func (w *fileWriter) performSingleDelete(ctx context.Context, path string) (tools.ToolResult, error) {
	info, statErr := w.fs.Stat(ctx, path)

	// Only snapshot if it is a file and not a directory
	if statErr == nil && !info.IsDir() {
		w.bm.snapshot(ctx, path, "DELETE")
	}

	if err := w.fs.Remove(ctx, path); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to delete path: %w", err)
	}

	return tools.ToolResult{Text: "Path deleted successfully."}, nil
}

func (w *fileWriter) createDirectory(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	resolvedPath, err := w.sm.IsPathWritable(params.Path)
	if err != nil {
		return tools.ToolResult{}, err
	}

	if err := w.fs.MkdirAll(ctx, resolvedPath, 0755); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to create directory: %w", err)
	}

	return tools.ToolResult{Text: "Directory created successfully."}, nil
}
