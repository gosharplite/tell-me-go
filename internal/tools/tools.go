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
	"time"

	"golang.org/x/term"
	"google.golang.org/genai"
)

var (
	safePaths           []string
	safePathsMu         sync.RWMutex
	safePathsFile       string // Path to persistent safe paths config
	readOnlyPaths       []string
	readOnlyPathsMu     sync.RWMutex
	readOnlyPathsFile   string // Path to persistent read-only paths config
	bypassFile          string // Path to persistent bypass state
	commandsLogFile     string // Path to log executed commands
	bypassConfirmations bool   // Skip all interactive confirmations
	bypassMu            sync.RWMutex
	TerminalMutex       sync.Mutex
)

// SetBypassFile sets the file where persistent bypass state is stored.
func SetBypassFile(path string) {
	bypassMu.Lock()
	defer bypassMu.Unlock()
	bypassFile = path
}

// LoadBypassState reads the persistent bypass state from disk.
func LoadBypassState() {
	bypassMu.Lock()
	defer bypassMu.Unlock()
	if bypassFile == "" {
		return
	}
	data, err := os.ReadFile(bypassFile)
	if err == nil {
		bypassConfirmations = strings.TrimSpace(string(data)) == "true"
	}
}

// IsBypassActive returns the current state of bypass_confirmation.
func IsBypassActive() bool {
	bypassMu.RLock()
	defer bypassMu.RUnlock()
	return bypassConfirmations
}

// SaveBypassState writes the persistent bypass state to disk.
func SaveBypassState() {
	bypassMu.RLock()
	file := bypassFile
	active := bypassConfirmations
	bypassMu.RUnlock()

	if file == "" {
		return
	}
	val := "false"
	if active {
		val = "true"
	}
	_ = AtomicWrite(file, []byte(val), 0644)
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

	// Try to open /dev/tty for interaction to avoid consuming Stdin if possible
	// However, term.MakeRaw typically works on Stdin's FD.
	fd := int(os.Stdin.Fd())

	// Check if Stdin is a terminal
	if !term.IsTerminal(fd) {
		// If not a terminal, we can't switch to raw mode.
		// Just read one byte from stdin directly.
		b := make([]byte, 1)
		_, err := os.Stdin.Read(b)
		if err != nil {
			return "", err
		}
		return strings.ToLower(string(b)), nil
	}

	// Switch to raw mode
	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	b := make([]byte, 1)
	_, err = os.Stdin.Read(b)
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
	TerminalMutex.Lock()
	defer TerminalMutex.Unlock()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if IsBypassActive() {
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

// SetReadOnlyPathsFile sets the file where persistent read-only paths are stored.
func SetReadOnlyPathsFile(path string) {
	readOnlyPathsMu.Lock()
	defer readOnlyPathsMu.Unlock()
	readOnlyPathsFile = path
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

// LoadReadOnlyPaths reads persistent read-only paths from disk.
func LoadReadOnlyPaths() error {
	readOnlyPathsMu.RLock()
	file := readOnlyPathsFile
	readOnlyPathsMu.RUnlock()

	if file == "" {
		return nil
	}

	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("failed to read read-only paths file: %w", err)
	}

	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return fmt.Errorf("failed to parse read-only paths JSON: %w", err)
	}

	for _, p := range paths {
		RegisterReadOnlyPath(p)
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

	return AtomicWrite(file, data, 0644)
}

// SaveReadOnlyPaths writes persistent read-only paths to disk.
func SaveReadOnlyPaths() error {
	readOnlyPathsMu.RLock()
	file := readOnlyPathsFile
	paths := make([]string, len(readOnlyPaths))
	copy(paths, readOnlyPaths)
	readOnlyPathsMu.RUnlock()

	if file == "" {
		return nil
	}

	data, err := json.MarshalIndent(paths, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal read-only paths: %w", err)
	}

	return AtomicWrite(file, data, 0644)
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

// RegisterReadOnlyPath adds a directory or file to the list of allowed boundaries for read-only access.
func RegisterReadOnlyPath(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	readOnlyPathsMu.Lock()
	defer readOnlyPathsMu.Unlock()

	// Check for duplicates
	for _, p := range readOnlyPaths {
		if p == abs {
			return
		}
	}
	readOnlyPaths = append(readOnlyPaths, abs)
}

// GetSafePaths returns a copy of the currently registered safe paths.
func GetSafePaths() []string {
	safePathsMu.RLock()
	defer safePathsMu.RUnlock()
	paths := make([]string, len(safePaths))
	copy(paths, safePaths)
	return paths
}

// GetReadOnlyPaths returns a copy of the currently registered read-only paths.
func GetReadOnlyPaths() []string {
	readOnlyPathsMu.RLock()
	defer readOnlyPathsMu.RUnlock()
	paths := make([]string, len(readOnlyPaths))
	copy(paths, readOnlyPaths)
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

// RemoveReadOnlyPath removes a path from the read-only authorized list.
func RemoveReadOnlyPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	readOnlyPathsMu.Lock()
	defer readOnlyPathsMu.Unlock()

	newPaths := []string{}
	found := false
	for _, p := range readOnlyPaths {
		if p == abs {
			found = true
			continue
		}
		newPaths = append(newPaths, p)
	}

	if !found {
		return fmt.Errorf("path '%s' not found in read-only authorized list", abs)
	}

	readOnlyPaths = newPaths
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

	if readOnlyPathsFile != "" {
		absReadSafeFile, err := filepath.Abs(readOnlyPathsFile)
		if err == nil && absPath == absReadSafeFile {
			return fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
		}
	}

	for _, sp := range safePaths {
		relSafe, err := filepath.Rel(sp, absPath)
		if err == nil && !strings.HasPrefix(relSafe, "..") && !filepath.IsAbs(relSafe) {
			return nil
		}
	}

	// 5. Allow paths within explicitly registered read-only paths
	readOnlyPathsMu.RLock()
	defer readOnlyPathsMu.RUnlock()

	for _, rop := range readOnlyPaths {
		relReadSafe, err := filepath.Rel(rop, absPath)
		if err == nil && !strings.HasPrefix(relReadSafe, "..") && !filepath.IsAbs(relReadSafe) {
			return nil
		}
	}

	return fmt.Errorf("security violation: path '%s' is outside allowed boundaries (CWD, Temp, or registered paths)", path)
}

// IsPathWritable checks if a path is within the writable boundaries (CWD, Temp, or registered safe paths).
// It does NOT include read-only paths.
func IsPathWritable(path string) error {
	if path == "" {
		return nil
	}

	path = filepath.Clean(path)

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

	if realPath, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = realPath
	}

	// 1. Allow paths within the Current Working Directory
	rel, err := filepath.Rel(cwd, absPath)
	if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return nil
	}

	// 2. Allow paths within the System Temp Directory
	tempDir := os.TempDir()
	absTemp, err := filepath.Abs(tempDir)
	if err == nil {
		relTemp, err := filepath.Rel(absTemp, absPath)
		if err == nil && !strings.HasPrefix(relTemp, "..") && !filepath.IsAbs(relTemp) {
			return nil
		}
	}

	// 3. Allow paths within explicitly registered safe paths
	safePathsMu.RLock()
	defer safePathsMu.RUnlock()

	if safePathsFile != "" {
		absSafeFile, err := filepath.Abs(safePathsFile)
		if err == nil && absPath == absSafeFile {
			return fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	if readOnlyPathsFile != "" {
		absReadSafeFile, err := filepath.Abs(readOnlyPathsFile)
		if err == nil && absPath == absReadSafeFile {
			return fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
		}
	}

	for _, sp := range safePaths {
		relSafe, err := filepath.Rel(sp, absPath)
		if err == nil && !strings.HasPrefix(relSafe, "..") && !filepath.IsAbs(relSafe) {
			return nil
		}
	}

	return fmt.Errorf("security violation: path '%s' is not in a writable boundary (read-only or unregistered)", path)
}

// ToolFunc is the signature for Go functions that can be called by the model.
type ToolFunc func(args map[string]interface{}) (string, error)

// ToolOptions defines execution behavior for a tool.
type ToolOptions struct {
	Serial bool // If true, the agent waits for this tool to finish before running others.
}

// toolEntry stores a tool's definition, handler, and execution options.
type toolEntry struct {
	declaration *genai.FunctionDeclaration
	handler     ToolFunc
	options     ToolOptions
}

// Registry maintains a mapping between function names and their Go implementations.
type Registry struct {
	declarations []*genai.FunctionDeclaration
	entries      map[string]toolEntry
}

// NewRegistry initializes an empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		declarations: make([]*genai.FunctionDeclaration, 0),
		entries:      make(map[string]toolEntry),
	}
}

// Register adds a new tool to the registry with default options.
func (r *Registry) Register(def *genai.FunctionDeclaration, handler ToolFunc) {
	r.RegisterWithOptions(def, handler, ToolOptions{})
}

// RegisterWithOptions adds a new tool to the registry with specific options.
func (r *Registry) RegisterWithOptions(def *genai.FunctionDeclaration, handler ToolFunc, opts ToolOptions) {
	r.declarations = append(r.declarations, def)
	r.entries[def.Name] = toolEntry{
		declaration: def,
		handler:     handler,
		options:     opts,
	}
}

// GetDeclarations returns the list of function declarations.
func (r *Registry) GetDeclarations() []*genai.FunctionDeclaration {
	return r.declarations
}

// Execute looks up and runs a tool handler with the provided JSON-parsed arguments.
func (r *Registry) Execute(name string, args map[string]interface{}) (string, error) {
	entry, ok := r.entries[name]
	if !ok {
		return "", fmt.Errorf("tool not found: %s", name)
	}
	return entry.handler(args)
}

// IsSerial returns true if the tool is configured for serial execution.
func (r *Registry) IsSerial(name string) bool {
	if entry, ok := r.entries[name]; ok {
		return entry.options.Serial
	}
	return false
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

// AtomicWrite writes data to a temporary file and then renames it to the target path.
// This ensures that the target file is either fully updated or not updated at all.
// It accepts a permission mode for the file (e.g., 0600 for secrets, 0644 for public).
func AtomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("failed to open temp file: %w", err)
	}

	// Ensure cleanup of the temp file on failure
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmp)
		}
	}()

	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Force flush to disk to prevent stale reads
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return fmt.Errorf("failed to sync temp file: %w", err)
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	cleanup = false // Rename succeeded, no need to remove
	return nil
}
