// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type fileSystemManager struct {
	sm *SecurityManager
	bm *BackupManager
	fs fsutil.FileSystem
}

// RegisterFileSystemTools adds file-related tools to the registry.
func RegisterFileSystemTools(r *Registry, sm *SecurityManager) {
	bm := NewBackupManager(sm, 10)
	m := &fileSystemManager{sm: sm, bm: bm, fs: fsutil.DefaultFileSystem}

	r.Register(&types.ToolDeclaration{
		Name:        "list_files",
		Description: "Returns a shallow list of filenames and directory names in a specific path. Useful for confirming file existence before reading.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to list (defaults to current directory '.')",
				},
			},
		},
	}, m.listFiles)

	r.Register(&types.ToolDeclaration{
		Name:        "get_tree",
		Description: "Returns a visual directory tree structure.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to list (defaults to current directory '.')",
				},
				"max_depth": {
					Type:        "INTEGER",
					Description: "Depth of the tree (default 2)",
				},
			},
		},
	}, m.getTree)

	r.Register(&types.ToolDeclaration{
		Name:        "read_file",
		Description: "Reads the full content of a specific file.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to read.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readFile)

	r.Register(&types.ToolDeclaration{
		Name:        "search_files",
		Description: "Performs a recursive regex search for a text pattern within a specific subdirectory. Use this when the search scope is restricted to a known module or folder.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory to search (defaults to '.')",
				},
				"query": {
					Type:        "STRING",
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, m.searchFiles)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "replace_text",
		Description: "Replaces the first occurrence of a specific text block in a file with new content.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to edit.",
				},
				"old_text": {
					Type:        "STRING",
					Description: "The exact text block to find and replace.",
				},
				"new_text": {
					Type:        "STRING",
					Description: "The new text to insert.",
				},
			},
			Required: []string{"filepath", "old_text", "new_text"},
		},
	}, m.replaceText, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "find_file",
		Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go'). Useful for locating specific configuration or source files by name.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to start the search (defaults to '.')",
				},
				"pattern": {
					Type:        "STRING",
					Description: "The file name pattern to search for (e.g., 'config.*').",
				},
			},
			Required: []string{"pattern"},
		},
	}, m.findFile)

	r.Register(&types.ToolDeclaration{
		Name:        "grep_definitions",
		Description: "Performs a regex-based search for symbol declarations (func, type, class) across files. Faster than AST tools for broad navigation but may return false positives.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to search.",
				},
				"query": {
					Type:        "STRING",
					Description: "Optional name pattern to filter definitions (regex).",
				},
			},
		},
	}, m.grepDefinitions)

	r.Register(&types.ToolDeclaration{
		Name:        "get_file_skeleton",
		Description: "Extracts the public API surface of a source file, including all exported types and function signatures, while omitting implementations.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the source code file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.getFileSkeleton)

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "write_file",
		Description: "Creates a new file or overwrites an existing one with the provided content. Automatically creates parent directories.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to write.",
				},
				"content": {
					Type:        "STRING",
					Description: "The full content to write to the file.",
				},
			},
			Required: []string{"filepath", "content"},
		},
	}, m.writeFile, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "append_text",
		Description: "Appends text to the end of a file. Efficient for logs or lists; avoids reading the whole file.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
				"content": {
					Type:        "STRING",
					Description: "The text to append. Ensure you include a leading newline (\\n) if starting a new line.",
				},
			},
			Required: []string{"filepath", "content"},
		},
	}, m.appendText, ToolOptions{Serial: true})

	r.Register(&types.ToolDeclaration{
		Name:        "get_file_diff",
		Description: "Generates a standard unified diff between two arbitrary file paths on disk. Does not require Git history.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"file1": {
					Type:        "STRING",
					Description: "The first file to compare.",
				},
				"file2": {
					Type:        "STRING",
					Description: "The second file to compare.",
				},
			},
			Required: []string{"file1", "file2"},
		},
	}, m.getFileDiff)

	r.Register(&types.ToolDeclaration{
		Name:        "undo_file_change",
		Description: "Reverts the last N file modifications (WRITE or REPLACE actions).",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"n": {
					Type:        "INTEGER",
					Description: "Number of changes to revert (default 1).",
				},
			},
		},
	}, m.undoFileChange)
}

func (m *fileSystemManager) listFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path string `json:"path"`
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

	entries, err := m.fs.ReadDir(ctx, resolvedPath)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to list directory: %w", err)
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

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *fileSystemManager) getTree(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Path     string `json:"path"`
		MaxDepth int    `json:"max_depth"`
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

	maxDepth := params.MaxDepth
	if maxDepth <= 0 {
		maxDepth = 2
	}

	var sb strings.Builder
	err = buildTree(ctx, m.fs, resolvedPath, "", 0, maxDepth, &sb)
	if err != nil {
		return types.ToolResult{}, err
	}
	return types.ToolResult{Text: sb.String()}, nil
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

func (m *fileSystemManager) readFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.FilePath
	if path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := m.sm.IsPathSafe(path)
	if err != nil {
		return types.ToolResult{}, err
	}

	content, err := m.fs.ReadFile(ctx, resolvedPath)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) > 100000 {
		return types.ToolResult{Text: string(content[:100000]) + "\n... (truncated)"}, nil
	}

	return types.ToolResult{Text: string(content)}, nil
}

func (m *fileSystemManager) writeFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
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
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "WRITE FILE", resolvedPath, content)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	m.bm.Snapshot(resolvedPath, "WRITE")

	// Create parent directories if they don't exist
	dir := filepath.Dir(resolvedPath)
	if err := m.fs.MkdirAll(ctx, dir, 0755); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to create directories: %w", err)
	}

	err = m.fs.WriteFile(ctx, resolvedPath, []byte(content), 0644)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return types.ToolResult{Text: "File written successfully."}, nil
}
