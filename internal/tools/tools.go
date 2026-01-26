// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"google.golang.org/genai"
)

var (
	safePaths     []string
	safePathsMu   sync.RWMutex
	safePathsFile string // Path to persistent safe paths config
)

// SetSafePathsFile sets the file where persistent safe paths are stored.
func SetSafePathsFile(path string) {
	safePathsMu.Lock()
	defer safePathsMu.Unlock()
	safePathsFile = path
}

// LoadSafePaths reads persistent safe paths from disk.
func LoadSafePaths() error {
	safePathsMu.RLock()
	file := safePathsFile
	safePathsMu.RUnlock()

	if file == "" {
		return nil
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read safe paths file: %w", err)
	}

	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return fmt.Errorf("failed to parse safe paths JSON: %w", err)
	}

	for _, p := range paths {
		RegisterSafePath(p)
	}
	return nil
}

// SaveSafePaths writes persistent safe paths to disk.
func SaveSafePaths() error {
	safePathsMu.RLock()
	file := safePathsFile
	paths := make([]string, len(safePaths))
	copy(paths, safePaths)
	safePathsMu.RUnlock()

	if file == "" {
		return nil
	}

	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal safe paths: %w", err)
	}

	return os.WriteFile(file, data, 0600) // Restricted permissions
}

// RegisterSafePath adds a directory or file to the list of allowed boundaries for tool access.
func RegisterSafePath(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	safePathsMu.Lock()
	defer safePathsMu.Unlock()

	// Check for duplicates
	for _, p := range safePaths {
		if p == abs {
			return
		}
	}
	safePaths = append(safePaths, abs)
}

// GetSafePaths returns a copy of the currently registered safe paths.
func GetSafePaths() []string {
	safePathsMu.RLock()
	defer safePathsMu.RUnlock()
	paths := make([]string, len(safePaths))
	copy(paths, safePaths)
	return paths
}

// RemoveSafePath removes a path from the authorized list.
func RemoveSafePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	safePathsMu.Lock()
	defer safePathsMu.Unlock()

	newPaths := []string{}
	found := false
	for _, p := range safePaths {
		if p == abs {
			found = true
			continue
		}
		newPaths = append(newPaths, p)
	}

	if !found {
		return fmt.Errorf("path '%s' not found in authorized list", abs)
	}

	safePaths = newPaths
	return nil
}

// IsPathSafe checks if a path is within the allowed boundaries (CWD, Temp, or registered Home/Config paths).
func IsPathSafe(path string) error {
	if path == "" {
		return nil
	}

	// 0. Hardened Sanitation: Explicitly clean the path first to resolve '..' and '.'
	path = filepath.Clean(path)

	// Handle potential flag-based paths (e.g., --file=/path)
	if strings.Contains(path, "=") {
		parts := strings.SplitN(path, "=", 2)
		if len(parts) == 2 {
			path = parts[1]
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// 1. Symlink Attack Mitigation:
	// If the file exists, evaluate its real path to prevent symlink-based traversal.
	// If it doesn't exist (e.g., for write_file), we proceed with the absolute path string.
	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}

	// 2. Allow paths within the Current Working Directory
	rel, err := filepath.Rel(cwd, absPath)
	if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return nil
	}

	// 3. Allow paths within the System Temp Directory
	tempDir := os.TempDir()
	absTemp, err := filepath.Abs(tempDir)
	if err == nil {
		relTemp, err := filepath.Rel(absTemp, absPath)
		if err == nil && !strings.HasPrefix(relTemp, "..") && !filepath.IsAbs(relTemp) {
			return nil
		}
	}

	// 4. Allow paths within explicitly registered safe paths
	safePathsMu.RLock()
	defer safePathsMu.RUnlock()

	// CRITICAL: Block access to the safePathsFile itself for the AI
	if safePathsFile != "" {
		absSafeFile, err := filepath.Abs(safePathsFile)
		if err == nil && absPath == absSafeFile {
			return fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	for _, sp := range safePaths {
		relSafe, err := filepath.Rel(sp, absPath)
		if err == nil && !strings.HasPrefix(relSafe, "..") && !filepath.IsAbs(relSafe) {
			return nil
		}
	}

	return fmt.Errorf("security violation: path '%s' is outside allowed boundaries (CWD, Temp, or registered paths)", path)
}

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(args map[string]interface{}) (string, error)

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	declarations []*genai.FunctionDeclaration
	handlers     map[string]ToolFunc
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		declarations: make([]*genai.FunctionDeclaration, 0),
		handlers:     make(map[string]ToolFunc),
	}
}

// Register adds a new tool to the registry.
func (r *Registry) Register(def *genai.FunctionDeclaration, handler ToolFunc) {
	r.declarations = append(r.declarations, def)
	r.handlers[def.Name] = handler
}

// GetDeclarations returns the list of function declarations.
func (r *Registry) GetDeclarations() []*genai.FunctionDeclaration {
	return r.declarations
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(name string, args map[string]interface{}) (string, error) {
	handler, ok := r.handlers[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return handler(args)
}

// ToToolSDK converts declarations into the format expected by the GenAI SDK.
func (r *Registry) ToToolSDK() []*genai.Tool {
	if len(r.declarations) == 0 {
		return nil
	}
	return []*genai.Tool{
		{
			FunctionDeclarations: r.declarations,
		},
	}
}
