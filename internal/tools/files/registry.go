// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
)

type fileSystemManager struct {
	reader   *fileReader
	writer   *fileWriter
	search   *fileSearcher
	skeleton *fileSkeleton
}

// Register adds file-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
	bm := NewBackupManager(sm, 10)
	m := &fileSystemManager{
		reader:   &fileReader{sm: sm, fs: fsutil.DefaultFileSystem},
		writer:   &fileWriter{sm: sm, bm: bm, fs: fsutil.DefaultFileSystem},
		search:   &fileSearcher{sm: sm, fs: fsutil.DefaultFileSystem},
		skeleton: &fileSkeleton{sm: sm, fs: fsutil.DefaultFileSystem},
	}

	r.Register(&tools.ToolDeclaration{
		Name:        "list_files",
		Description: "Returns a shallow list of filenames and directory names in a specific path. Useful for confirming file existence before reading.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"path": {
					Type:        "STRING",
					Description: "The directory path to list (defaults to current directory '.')",
				},
			},
		},
	}, m.reader.listFiles)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_tree",
		Description: "Returns a visual directory tree structure.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.reader.getTree)

	r.Register(&tools.ToolDeclaration{
		Name:        "read_file",
		Description: "Reads the full content of a specific file.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to read.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.reader.readFile)

	r.Register(&tools.ToolDeclaration{
		Name:        "search_files",
		Description: "Performs a recursive regex search for a text pattern within a specific subdirectory. Use this when the search scope is restricted to a known module or folder.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.search.searchFiles)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "replace_text",
		Description: "Replaces the first occurrence of a specific text block in a file with new content.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
				"reason": {
					Type:        "STRING",
					Description: "Reason for this replacement.",
				},
			},
			Required: []string{"filepath", "old_text", "new_text", "reason"},
		},
	}, m.writer.replaceText, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "find_file",
		Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go'). Useful for locating specific configuration or source files by name.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.reader.findFile)

	r.Register(&tools.ToolDeclaration{
		Name:        "grep_definitions",
		Description: "Performs a regex-based search for symbol declarations (func, type, class) across files. Faster than AST tools for broad navigation but may return false positives.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.search.grepDefinitions)

	r.Register(&tools.ToolDeclaration{
		Name:        "get_file_skeleton",
		Description: "Extracts the public API surface of a source file, including all exported types and function signatures, while omitting implementations.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the source code file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.skeleton.getFileSkeleton)

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "write_file",
		Description: "Creates a new file or overwrites an existing one with the provided content. Automatically creates parent directories.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file to write.",
				},
				"content": {
					Type:        "STRING",
					Description: "The full content to write to the file.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for writing this file.",
				},
			},
			Required: []string{"filepath", "content", "reason"},
		},
	}, m.writer.writeFile, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "append_text",
		Description: "Appends text to the end of a file. Efficient for logs or lists; avoids reading the whole file.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"filepath": {
					Type:        "STRING",
					Description: "The path to the file.",
				},
				"content": {
					Type:        "STRING",
					Description: "The text to append. Ensure you include a leading newline (\\n) if starting a new line.",
				},
				"reason": {
					Type:        "STRING",
					Description: "Reason for appending this text.",
				},
			},
			Required: []string{"filepath", "content", "reason"},
		},
	}, m.writer.appendText, registry.ToolOptions{Serial: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "get_file_diff",
		Description: "Generates a standard unified diff between two arbitrary file paths on disk. Does not require Git history.",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
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
	}, m.reader.getFileDiff)

	r.Register(&tools.ToolDeclaration{
		Name:        "undo_file_change",
		Description: "Reverts the last N file modifications (WRITE or REPLACE actions).",
		Parameters: &tools.Schema{
			Type: "OBJECT",
			Properties: map[string]*tools.Schema{
				"n": {
					Type:        "INTEGER",
					Description: "Number of changes to revert (default 1).",
				},
			},
		},
	}, m.writer.undoFileChange)
}
