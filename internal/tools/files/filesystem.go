// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package files

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/fsutil"
	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type fileSystemManager struct {
	sm *security.SecurityManager
	bm *BackupManager
	fs fsutil.FileSystem
}

// Register adds file-related tools to the registry.
func Register(r *registry.Registry, sm *security.SecurityManager) {
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
	}, m.replaceText, registry.ToolOptions{Serial: true, LongRunning: true})

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
	}, m.writeFile, registry.ToolOptions{Serial: true, LongRunning: true})

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
	}, m.appendText, registry.ToolOptions{Serial: true})

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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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

	var results []string

	// Prepare Regex for Fallback
	var reQuery *regexp.Regexp
	if params.Query != "" {
		reQuery, _ = regexp.Compile("(?i)" + params.Query)
	}

	// Fallback Walk (Non-Go files)
	processor := func(filePath string) error {
		ext := filepath.Ext(filePath)
		if !isSupportedDefExt(ext) {
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
	return ext == ".py" || ext == ".js" || ext == ".sh" || ext == ".md" || ext == ".go"
}

func formatMatch(path string, lineNum int, text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > 500 {
		trimmed = trimmed[:500] + " (truncated)"
	}
	return fmt.Sprintf("%s:%d: %s", path, lineNum, trimmed)
}

func (m *fileSystemManager) replaceText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
		OldText  string `json:"old_text"`
		NewText  string `json:"new_text"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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

var skeletonPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^func\s+`),
	regexp.MustCompile(`^type\s+`),
	regexp.MustCompile(`^def\s+`),
	regexp.MustCompile(`^class\s+`),
	regexp.MustCompile(`^function\s+`),
	regexp.MustCompile(`^\w+\(\)\s*\{`),
}

func (m *fileSystemManager) getFileSkeleton(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		FilePath string `json:"filepath"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}
	if params.FilePath == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	resolvedPath, err := m.sm.IsPathSafe(params.FilePath)
	if err != nil {
		return types.ToolResult{}, err
	}

	return m.extractGenericSkeleton(ctx, resolvedPath)
}

func (m *fileSystemManager) extractGenericSkeleton(ctx context.Context, path string) (types.ToolResult, error) {
	file, err := m.fs.Open(ctx, path)
	if err != nil {
		return types.ToolResult{}, err
	}
	defer file.Close()

	ext := filepath.Ext(path)
	skeleton, err := scanForDefinitions(file, ext)
	if err != nil {
		return types.ToolResult{}, err
	}

	if skeleton == "" {
		return types.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}
	return types.ToolResult{Text: skeleton}, nil
}

func scanForDefinitions(r io.Reader, ext string) (string, error) {
	scanner := bufio.NewScanner(r)
	var sb strings.Builder
	var lastComments []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") {
			lastComments = append(lastComments, line)
			continue
		}

		if trimmed == "" {
			lastComments = nil
			continue
		}

		isDef := false
		for _, p := range skeletonPatterns {
			if p.MatchString(line) {
				isDef = true
				break
			}
		}

		if isDef {
			for _, c := range lastComments {
				sb.WriteString(c + "\n")
			}
			sb.WriteString(line + "\n")
			sb.WriteString("\n")
		}
		lastComments = nil
	}
	return sb.String(), scanner.Err()
}
