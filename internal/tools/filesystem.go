// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"fmt"
	"os"
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
}

func listFiles(args map[string]interface{}) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		path = "."
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

func replaceText(args map[string]interface{}) (string, error) {
	path, _ := args["filepath"].(string)
	oldText, _ := args["old_text"].(string)
	newText, _ := args["new_text"].(string)

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
