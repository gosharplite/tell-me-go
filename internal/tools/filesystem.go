// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

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
	"github.com/gosharplite/tell-me-go/internal/types"
	"google.golang.org/genai"
)

type fileSystemManager struct {
	sm *SecurityManager
	bm *BackupManager
}

// RegisterFileSystemTools adds file-related tools to the registry.
func RegisterFileSystemTools(r *Registry, sm *SecurityManager) {
	bm := NewBackupManager(sm, 10)
	m := &fileSystemManager{sm: sm, bm: bm}

	r.Register(&genai.FunctionDeclaration{
		Name:        "list_files",
		Description: "Returns a shallow list of filenames and directory names in a specific path. Useful for confirming file existence before reading.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory path to list (defaults to current directory '.')",
				},
			},
		},
	}, m.listFiles)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_tree",
		Description: "Returns a visual directory tree structure.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory path to list (defaults to current directory '.')",
				},
				"max_depth": {
					Type:        genai.TypeInteger,
					Description: "Depth of the tree (default 2)",
				},
			},
		},
	}, m.getTree)

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_file",
		Description: "Reads the full content of a specific file.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the file to read.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.readFile)

	r.Register(&genai.FunctionDeclaration{
		Name:        "search_files",
		Description: "Performs a recursive regex search for a text pattern within a specific subdirectory. Use this when the search scope is restricted to a known module or folder.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory to search (defaults to '.')",
				},
				"query": {
					Type:        genai.TypeString,
					Description: "The string or regex to search for.",
				},
			},
			Required: []string{"query"},
		},
	}, m.searchFiles)

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "replace_text",
		Description: "Replaces the first occurrence of a specific text block in a file with new content.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the file to edit.",
				},
				"old_text": {
					Type:        genai.TypeString,
					Description: "The exact text block to find and replace.",
				},
				"new_text": {
					Type:        genai.TypeString,
					Description: "The new text to insert.",
				},
			},
			Required: []string{"filepath", "old_text", "new_text"},
		},
	}, m.replaceText, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "find_file",
		Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go'). Useful for locating specific configuration or source files by name.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory path to start the search (defaults to '.')",
				},
				"pattern": {
					Type:        genai.TypeString,
					Description: "The file name pattern to search for (e.g., 'config.*').",
				},
			},
			Required: []string{"pattern"},
		},
	}, m.findFile)

	r.Register(&genai.FunctionDeclaration{
		Name:        "grep_definitions",
		Description: "Performs a regex-based search for symbol declarations (func, type, class) across files. Faster than AST tools for broad navigation but may return false positives.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory path to search.",
				},
				"query": {
					Type:        genai.TypeString,
					Description: "Optional name pattern to filter definitions (regex).",
				},
			},
		},
	}, m.grepDefinitions)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_file_skeleton",
		Description: "Extracts the public API surface of a source file, including all exported types and function signatures, while omitting implementations.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the source code file.",
				},
			},
			Required: []string{"filepath"},
		},
	}, m.getFileSkeleton)

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "write_file",
		Description: "Creates a new file or overwrites an existing one with the provided content. Automatically creates parent directories.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"filepath": {
					Type:        genai.TypeString,
					Description: "The path to the file to write.",
				},
				"content": {
					Type:        genai.TypeString,
					Description: "The full content to write to the file.",
				},
			},
			Required: []string{"filepath", "content"},
		},
	}, m.writeFile, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_file_diff",
		Description: "Generates a standard unified diff between two arbitrary file paths on disk. Does not require Git history.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"file1": {
					Type:        genai.TypeString,
					Description: "The first file to compare.",
				},
				"file2": {
					Type:        genai.TypeString,
					Description: "The second file to compare.",
				},
			},
			Required: []string{"file1", "file2"},
		},
	}, m.getFileDiff)

	r.Register(&genai.FunctionDeclaration{
		Name:        "undo_file_change",
		Description: "Reverts the last N file modifications (WRITE or REPLACE actions).",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"n": {
					Type:        genai.TypeInteger,
					Description: "Number of changes to revert (default 1).",
				},
			},
		},
	}, m.undoFileChange)
}

func (m *fileSystemManager) listFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to list directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contents of %s:\n", path))
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
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	maxDepth := 2
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}

	var sb strings.Builder
	err := buildTree(path, "", 0, maxDepth, &sb)
	if err != nil {
		return types.ToolResult{}, err
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func buildTree(path, indent string, depth, maxDepth int, sb *strings.Builder) error {
	if depth > maxDepth {
		return nil
	}
	entries, err := os.ReadDir(path)
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
			buildTree(filepath.Join(path, entry.Name()), newIndent, depth+1, maxDepth, sb)
		}
	}
	return nil
}

func (m *fileSystemManager) readFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to read file: %w", err)
	}

	if len(content) > 100000 {
		return types.ToolResult{Text: string(content[:100000]) + "\n... (truncated)"}, nil
	}

	return types.ToolResult{Text: string(content)}, nil
}

func (m *fileSystemManager) searchFiles(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return types.ToolResult{}, fmt.Errorf("query argument is required")
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil
		}
		defer file.Close()

		// Read first 1024 bytes to check if binary
		buf := make([]byte, 1024)
		n, err := file.Read(buf)
		if err != nil && err != io.EOF {
			return nil // Skip file on error
		}
		if isBinary(buf[:n]) {
			return nil
		}
		file.Seek(0, 0)

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if re.MatchString(line) {
				trimmed := strings.TrimSpace(line)
				if len(trimmed) > 500 {
					trimmed = trimmed[:500] + " (truncated)"
				}
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, lineNum, trimmed))
				if len(results) > 100 {
					return fmt.Errorf("too many results")
				}
			}
		}
		return nil
	})

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

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

func (m *fileSystemManager) getFileDiff(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	file1, _ := args["file1"].(string)
	file2, _ := args["file2"].(string)

	if err := m.sm.IsPathSafe(file1); err != nil {
		return types.ToolResult{}, err
	}
	if err := m.sm.IsPathSafe(file2); err != nil {
		return types.ToolResult{}, err
	}

	cmd := exec.CommandContext(ctx, "diff", "-u", file1, file2)
	out, _ := cmd.CombinedOutput()

	if len(out) == 0 {
		return types.ToolResult{Text: "Files are identical."}, nil
	}

	return types.ToolResult{Text: string(out)}, nil
}

func (m *fileSystemManager) writeFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, _ := args["filepath"].(string)
	content, _ := args["content"].(string)

	if err := m.sm.IsPathWritable(path); err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "WRITE FILE", path, content)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	m.bm.Snapshot(path, "WRITE")

	// Create parent directories if they don't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to create directories: %w", err)
	}

	err = fsutil.AtomicWrite(path, []byte(content), 0644)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to write file: %w", err)
	}

	return types.ToolResult{Text: "File written successfully."}, nil
}

func (m *fileSystemManager) replaceText(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, _ := args["filepath"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)

	if err := m.sm.IsPathWritable(path); err != nil {
		return types.ToolResult{}, err
	}

	// Confirmation Gate
	detail := fmt.Sprintf("Replace (first occurrence):\n%s\nWith:\n%s", oldText, newText)
	approved, err := m.sm.ConfirmDestructiveAction(ctx, "REPLACE TEXT", path, detail)
	if err != nil {
		return types.ToolResult{}, err
	}
	if !approved {
		return types.ToolResult{Text: "Action denied by user."}, nil
	}

	m.bm.Snapshot(path, "REPLACE")

	contentBytes, err := os.ReadFile(path)
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
	err = fsutil.AtomicWrite(path, []byte(newContent), 0644)
	if err != nil {
		return types.ToolResult{}, err
	}

	return types.ToolResult{Text: "File updated successfully."}, nil
}

func (m *fileSystemManager) findFile(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return types.ToolResult{}, fmt.Errorf("pattern argument is required")
	}

	var results []string
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		matched, err := filepath.Match(pattern, info.Name())
		if err != nil {
			return err
		}

		if matched {
			results = append(results, filePath)
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No files found matching pattern."}, nil
	}

	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *fileSystemManager) grepDefinitions(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	query, _ := args["query"].(string)

	// Attempt AST-based search for Go files first
	astResults, err := grepDefinitionsGo(path, query)
	if err != nil {
		// Fallback to regex if AST fails for some reason
	}

	// Broad definition patterns for non-Go files
	defPatterns := []string{
		`^def\s+\w+`,            // Python function
		`^class\s+\w+`,          // Python/JS class
		`^function\s+`,          // JS function
		`^const\s+\w+\s*=\s*\(`, // JS arrow function
		`^\w+\(\)\s*\{`,         // Bash function
	}

	var reQuery *regexp.Regexp
	if query != "" {
		var err error
		reQuery, err = regexp.Compile("(?i)" + query)
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("invalid query regex: %w", err)
		}
	}

	var results []string
	results = append(results, astResults...)

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		// Only check common source files, skip .go if we already handled them with AST (which we do by default now)
		ext := filepath.Ext(filePath)
		if ext != ".py" && ext != ".js" && ext != ".sh" && ext != ".md" {
			return nil
		}

		file, err := os.Open(filePath)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			isDef := false
			for _, p := range defPatterns {
				if matched, _ := regexp.MatchString(p, line); matched {
					isDef = true
					break
				}
			}

			if isDef {
				if reQuery == nil || reQuery.MatchString(line) {
					trimmed := strings.TrimSpace(line)
					if len(trimmed) > 500 {
						trimmed = trimmed[:500] + " (truncated)"
					}
					results = append(results, fmt.Sprintf("%s:%d: %s", filePath, lineNum, trimmed))
				}
			}
		}
		return nil
	})

	if err != nil {
		return types.ToolResult{}, err
	}

	if len(results) == 0 {
		return types.ToolResult{Text: "No definitions found."}, nil
	}

	return types.ToolResult{Text: strings.Join(results, "\n")}, nil
}

func (m *fileSystemManager) getFileSkeleton(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return types.ToolResult{}, fmt.Errorf("filepath argument is required")
	}

	if err := m.sm.IsPathSafe(path); err != nil {
		return types.ToolResult{}, err
	}

	if filepath.Ext(path) == ".go" {
		skeleton, err := getFileSkeletonGo(path)
		if err == nil {
			return types.ToolResult{Text: skeleton}, nil
		}
		// Fallback to heuristic if AST fails
	}

	file, err := os.Open(path)
	if err != nil {
		return types.ToolResult{}, err
	}
	defer file.Close()

	ext := filepath.Ext(path)
	scanner := bufio.NewScanner(file)
	var sb strings.Builder
	var lastComments []string

	// Simple heuristic: extract lines that look like definitions and their preceding comments
	defPatterns := []string{
		`^func\s+`, `^type\s+`, `^def\s+`, `^class\s+`, `^function\s+`, `^\w+\(\)\s*\{`,
	}

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
		for _, p := range defPatterns {
			if matched, _ := regexp.MatchString(p, line); matched {
				isDef = true
				break
			}
		}

		if isDef {
			for _, c := range lastComments {
				sb.WriteString(c + "\n")
			}
			sb.WriteString(line + "\n")
			if ext == ".py" && strings.HasSuffix(trimmed, ":") {
				// Keep going for Python
			} else if !strings.HasSuffix(trimmed, "{") && ext != ".py" {
				// Might be a multi-line signature or type, but we keep it simple
			}
			sb.WriteString("\n")
		}
		lastComments = nil
	}

	out := sb.String()
	if out == "" {
		return types.ToolResult{Text: "Could not extract skeleton or file has no recognized definitions."}, nil
	}

	return types.ToolResult{Text: out}, nil
}

func (m *fileSystemManager) undoFileChange(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	n := 1
	if val, ok := args["n"].(float64); ok {
		n = int(val)
	}
	res, err := m.bm.Undo(n)
	return types.ToolResult{Text: res}, err
}
