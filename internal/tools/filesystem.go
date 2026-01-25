// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"google.golang.org/genai"
)

// RegisterFileSystemTools adds file-related tools to the registry.
func RegisterFileSystemTools(r *Registry) {
	r.Register(&genai.FunctionDeclaration{
		Name:        "list_files",
		Description: "Lists files and directories in the specified path.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The directory path to list (defaults to current directory '.')",
				},
			},
		},
	}, listFiles)

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
	}, getTree)

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
	}, readFile)

	r.Register(&genai.FunctionDeclaration{
		Name:        "search_files",
		Description: "Searches for a text pattern in files within a directory (recursive).",
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
	}, searchFiles)

	r.Register(&genai.FunctionDeclaration{
		Name:        "replace_text",
		Description: "Replaces a specific text block in a file with new content. Replaces ONLY the first occurrence found.",
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
	}, replaceText)

	r.Register(&genai.FunctionDeclaration{
		Name:        "find_file",
		Description: "Finds files based on name patterns using filepath.Match (e.g., '*.go').",
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
	}, findFile)

	r.Register(&genai.FunctionDeclaration{
		Name:        "grep_definitions",
		Description: "Searches for code definitions (functions, classes, structs) within files.",
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
	}, grepDefinitions)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_file_skeleton",
		Description: "Returns the skeleton (function signatures, classes, structs, and docstrings) of a source code file, omitting function bodies.",
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
	}, getFileSkeleton)

	r.Register(&genai.FunctionDeclaration{
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
	}, writeFile)

	r.Register(&genai.FunctionDeclaration{
		Name:        "get_file_diff",
		Description: "Compares two files and returns their differences.",
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
	}, getFileDiff)
}

func listFiles(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", fmt.Errorf("failed to list directory: %w", err)
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

	return sb.String(), nil
}

func getTree(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	maxDepth := 2
	if d, ok := args["max_depth"].(float64); ok {
		maxDepth = int(d)
	}

	var sb strings.Builder
	err := buildTree(path, "", 0, maxDepth, &sb)
	if err != nil {
		return "", err
	}
	return sb.String(), nil
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

func readFile(args map[string]interface{}) (string, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filepath argument is required")
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}

	return string(content), nil
}

func searchFiles(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query argument is required")
	}

	re, err := regexp.Compile(query)
	if err != nil {
		return "", fmt.Errorf("invalid regex: %w", err)
	}

	var results []string
	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "node_modules" || info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				results = append(results, fmt.Sprintf("%s:%d: %s", filePath, i+1, strings.TrimSpace(line)))
				if len(results) > 100 {
					return fmt.Errorf("too many results")
				}
			}
		}
		return nil
	})

	if err != nil && err.Error() != "too many results" {
		return "", err
	}

	if len(results) == 0 {
		return "No matches found.", nil
	}

	out := strings.Join(results, "\n")
	if err != nil && err.Error() == "too many results" {
		out += "\n... (truncated)"
	}
	return out, nil
}

func getFileDiff(args map[string]interface{}) (string, error) {
	file1, _ := args["file1"].(string)
	file2, _ := args["file2"].(string)

	if err := checkPathSafety(file1); err != nil {
		return "", err
	}
	if err := checkPathSafety(file2); err != nil {
		return "", err
	}

	cmd := exec.Command("diff", "-u", file1, file2)
	out, _ := cmd.CombinedOutput()

	if len(out) == 0 {
		return "Files are identical.", nil
	}

	return string(out), nil
}

func checkPathSafety(path string) error {
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	rel, err := filepath.Rel(cwd, absPath)
	if err != nil {
		return fmt.Errorf("failed to get relative path: %w", err)
	}

	if strings.HasPrefix(rel, "..") {
		return fmt.Errorf("security violation: path '%s' is outside the current working directory", path)
	}

	return nil
}

func writeFile(args map[string]interface{}) (string, error) {
	path, _ := args["filepath"].(string)
	content, _ := args["content"].(string)

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	// Create parent directories if they don't exist
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directories: %w", err)
	}

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return "File written successfully.", nil
}

func replaceText(args map[string]interface{}) (string, error) {
	path, _ := args["filepath"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	if !strings.Contains(string(content), oldText) {
		return "", fmt.Errorf("old_text not found in file")
	}

	newContent := strings.Replace(string(content), oldText, newText, 1)
	err = os.WriteFile(path, []byte(newContent), 0644)
	if err != nil {
		return "", err
	}

	return "File updated successfully.", nil
}

func findFile(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	pattern, ok := args["pattern"].(string)
	if !ok || pattern == "" {
		return "", fmt.Errorf("pattern argument is required")
	}

	var results []string
	err := filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
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
		return "", err
	}

	if len(results) == 0 {
		return "No files found matching pattern.", nil
	}

	return strings.Join(results, "\n"), nil
}

func grepDefinitions(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
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
			return "", fmt.Errorf("invalid query regex: %w", err)
		}
	}

	var results []string
	results = append(results, astResults...)

	err = filepath.Walk(path, func(filePath string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
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
					results = append(results, fmt.Sprintf("%s:%d: %s", filePath, lineNum, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	})

	if err != nil {
		return "", err
	}

	if len(results) == 0 {
		return "No definitions found.", nil
	}

	return strings.Join(results, "\n"), nil
}

func getFileSkeleton(args map[string]interface{}) (string, error) {
	path, ok := args["filepath"].(string)
	if !ok || path == "" {
		return "", fmt.Errorf("filepath argument is required")
	}

	if err := checkPathSafety(path); err != nil {
		return "", err
	}

	if filepath.Ext(path) == ".go" {
		skeleton, err := getFileSkeletonGo(path)
		if err == nil {
			return skeleton, nil
		}
		// Fallback to heuristic if AST fails
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
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
		return "Could not extract skeleton or file has no recognized definitions.", nil
	}

	return out, nil
}
