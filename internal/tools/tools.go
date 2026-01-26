// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package tools manages the registration and execution of function calls (tools)
// used by the Gemini model.
package tools

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"google.golang.org/genai"
)

var (
	safePaths           []string
	safePathsMu         sync.RWMutex
	safePathsFile       string // Path to persistent safe paths config
	bypassFile          string // Path to persistent bypass state
	commandsLogFile     string // Path to log executed commands
	bypassConfirmations bool   // Skip all interactive confirmations
	termMu              sync.Mutex
)

// SetBypassFile sets the file where persistent bypass state is stored.
func SetBypassFile(path string) {
	bypassFile = path
}

// LoadBypassState reads the persistent bypass state from disk.
func LoadBypassState() {
	if bypassFile == "" {
		return
	}
	data, err := os.ReadFile(bypassFile)
	if err == nil {
		bypassConfirmations = strings.TrimSpace(string(data)) == "true"
	}
}

// SaveBypassState writes the persistent bypass state to disk.
func SaveBypassState() {
	if bypassFile == "" {
		return
	}
	val := "false"
	if bypassConfirmations {
		val = "true"
	}
	_ = os.WriteFile(bypassFile, []byte(val), 0600)
}

// SetCommandsLogFile sets the path for logging executed commands.
func SetCommandsLogFile(path string) {
	commandsLogFile = path
}

// readSingleKey waits for a single key press from the user and returns it in lowercase.
func readSingleKey() (string, error) {
	// Support for E2E mocking of user input
	if val := os.Getenv("TELL_ME_MOCK_ANSWER"); val != "" {
		return strings.ToLower(val[:1]), nil
	}

	// Try to open /dev/tty for interaction to avoid consuming Stdin
	input := os.Stdin
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err == nil {
		input = tty
		defer tty.Close()
	}

	// Check if input is a terminal
	isTerm := false
	stat, err := input.Stat()
	if err == nil && (stat.Mode()&os.ModeCharDevice) != 0 {
		isTerm = true
	}

	if isTerm {
		// Disable input buffering for real terminal
		// We use /dev/tty specifically for stty to be sure
		flag := "-F" // Linux
		if runtime.GOOS == "darwin" || runtime.GOOS == "freebsd" || runtime.GOOS == "openbsd" {
			flag = "-f" // macOS and BSD
		}
		exec.Command("stty", flag, "/dev/tty", "cbreak", "min", "1").Run()
		// Restore input buffering on exit
		defer exec.Command("stty", flag, "/dev/tty", "-cbreak").Run()
	}

	var b []byte = make([]byte, 1)
	_, err = input.Read(b)
	if err != nil {
		return "", err
	}
	return strings.ToLower(string(b)), nil
}

// logAudit writes a two-line audit entry to the commands log file.
func logAudit(label1, val1, label2, val2 string) {
	if commandsLogFile == "" {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f, err := os.OpenFile(commandsLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[0;31m[Warning] Failed to open command log file: %v\033[0m\n", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label1, val1)
	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label2, val2)
}

// ConfirmDestructiveAction prompts the user for confirmation before performing a destructive tool action.
func ConfirmDestructiveAction(action, target, detail string) bool {
	termMu.Lock()
	defer termMu.Unlock()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if bypassConfirmations {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Action '%s' on '%s' auto-approved (bypass_confirmation enabled).\033[0m\n", action, target)
		logAudit("ACTION", action+" on "+target, "DETAIL", detailLog+" (auto-approved via bypass_confirmation)")
		return true
	}

	fmt.Fprintf(os.Stderr, "\033[1;33m[CONFIRMATION REQUIRED]\033[0m\n")
	fmt.Fprintf(os.Stderr, "AI is requesting to %s: %s\n", action, target)
	if detail != "" {
		if len(detail) > 1000 {
			detail = detail[:1000] + "\n... (truncated)"
		}
		fmt.Fprintf(os.Stderr, "\033[90m%s\033[0m\n", detail)
	}
	fmt.Fprintf(os.Stderr, "Proceed? (y/N) ")

	char, err := readSingleKey()
	fmt.Fprintf(os.Stderr, "\n")
	if err == nil && char == "y" {
		logAudit("ACTION", action+" on "+target, "DETAIL", detailLog)
		return true
	}
	return false
}

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
