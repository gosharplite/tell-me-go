// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

// SecurityManager encapsulates all security-related state and path validation logic.
type SecurityManager struct {
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
	terminalMu          sync.Mutex
	pricingMu           sync.Mutex
}

// NewSecurityManager initializes a new SecurityManager.
func NewSecurityManager() *SecurityManager {
	return &SecurityManager{}
}

// SetBypassFile sets the file where persistent bypass state is stored.
func (sm *SecurityManager) SetBypassFile(path string) {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	sm.bypassFile = path
}

// LoadBypassState reads the persistent bypass state from disk.
func (sm *SecurityManager) LoadBypassState() {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	if sm.bypassFile == "" {
		return
	}
	data, err := os.ReadFile(sm.bypassFile)
	if err == nil {
		sm.bypassConfirmations = strings.TrimSpace(string(data)) == "true"
	}
}

// IsBypassActive returns the current state of bypass_confirmation.
func (sm *SecurityManager) IsBypassActive() bool {
	sm.bypassMu.RLock()
	defer sm.bypassMu.RUnlock()
	return sm.bypassConfirmations
}

// SaveBypassState writes the persistent bypass state to disk.
func (sm *SecurityManager) SaveBypassState() {
	sm.bypassMu.RLock()
	file := sm.bypassFile
	active := sm.bypassConfirmations
	sm.bypassMu.RUnlock()

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
func (sm *SecurityManager) SetCommandsLogFile(path string) {
	sm.commandsLogFile = path
}

// TerminalLock locks the terminal for exclusive access.
func (sm *SecurityManager) TerminalLock() {
	sm.terminalMu.Lock()
}

// TerminalUnlock unlocks the terminal.
func (sm *SecurityManager) TerminalUnlock() {
	sm.terminalMu.Unlock()
}

// readSingleKey waits for a single key press from the user and returns it in lowercase.
func readSingleKey(ctx context.Context) (string, error) {
	// Check context before terminal check
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}

	// Support for E2E mocking of user input
	if val := os.Getenv("TELL_ME_MOCK_ANSWER"); val != "" {
		return strings.ToLower(val[:1]), nil
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("confirmation required but not running in a terminal. Use --bypass-confirmation to skip if running in a non-interactive environment")
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		return "", err
	}
	defer term.Restore(fd, state)

	type result struct {
		b   byte
		err error
	}
	resChan := make(chan result, 1)
	go func() {
		b := make([]byte, 1)
		_, err := os.Stdin.Read(b)
		if err != nil {
			resChan <- result{0, err}
		} else {
			resChan <- result{b[0], nil}
		}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resChan:
		if res.err != nil {
			return "", res.err
		}
		if res.b == 3 { // Ctrl+C (ETX)
			return "", fmt.Errorf("interrupted")
		}
		return strings.ToLower(string(res.b)), nil
	}
}

// logAudit writes a two-line audit entry to the commands log file.
func (sm *SecurityManager) logAudit(label1, val1, label2, val2 string) {
	if sm.commandsLogFile == "" {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	f, err := os.OpenFile(sm.commandsLogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[0;31m[Warning] Failed to open command log file: %v\033[0m\n", err)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label1, val1)
	fmt.Fprintf(f, "[%s] %s: %s\n", timestamp, label2, val2)
}

// ConfirmDestructiveAction prompts the user for confirmation before performing a destructive tool action.
func (sm *SecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	sm.TerminalLock()
	defer sm.TerminalUnlock()

	detailLog := detail
	if len(detailLog) > 500 {
		detailLog = detailLog[:500] + "... (truncated)"
	}

	if sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Action '%s' on '%s' auto-approved (bypass_confirmation enabled).\033[0m\n", action, target)
		sm.logAudit("ACTION", action+" on "+target, "DETAIL", detailLog+" (auto-approved via bypass_confirmation)")
		return true, nil
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

	char, err := readSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return false, err
	}
	if char == "y" {
		sm.logAudit("ACTION", action+" on "+target, "DETAIL", detailLog)
		return true, nil
	}
	return false, nil
}

// SetSafePathsFile sets the file where persistent safe paths are stored.
func (sm *SecurityManager) SetSafePathsFile(path string) {
	sm.safePathsMu.Lock()
	defer sm.safePathsMu.Unlock()
	sm.safePathsFile = path
}

// SetReadOnlyPathsFile sets the file where persistent read-only paths are stored.
func (sm *SecurityManager) SetReadOnlyPathsFile(path string) {
	sm.readOnlyPathsMu.Lock()
	defer sm.readOnlyPathsMu.Unlock()
	sm.readOnlyPathsFile = path
}

// LoadSafePaths reads persistent safe paths from disk.
func (sm *SecurityManager) LoadSafePaths() error {
	sm.safePathsMu.RLock()
	file := sm.safePathsFile
	sm.safePathsMu.RUnlock()

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
		sm.RegisterSafePath(p)
	}
	return nil
}

// LoadReadOnlyPaths reads persistent read-only paths from disk.
func (sm *SecurityManager) LoadReadOnlyPaths() error {
	sm.readOnlyPathsMu.RLock()
	file := sm.readOnlyPathsFile
	sm.readOnlyPathsMu.RUnlock()

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
		sm.RegisterReadOnlyPath(p)
	}
	return nil
}

// SaveSafePaths writes persistent safe paths to disk.
func (sm *SecurityManager) SaveSafePaths() error {
	sm.safePathsMu.RLock()
	file := sm.safePathsFile
	paths := make([]string, len(sm.safePaths))
	copy(paths, sm.safePaths)
	sm.safePathsMu.RUnlock()

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
func (sm *SecurityManager) SaveReadOnlyPaths() error {
	sm.readOnlyPathsMu.RLock()
	file := sm.readOnlyPathsFile
	paths := make([]string, len(sm.readOnlyPaths))
	copy(paths, sm.readOnlyPaths)
	sm.readOnlyPathsMu.RUnlock()

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
func (sm *SecurityManager) RegisterSafePath(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	sm.safePathsMu.Lock()
	defer sm.safePathsMu.Unlock()

	// Check for duplicates
	for _, p := range sm.safePaths {
		if p == abs {
			return
		}
	}
	sm.safePaths = append(sm.safePaths, abs)
}

// RegisterReadOnlyPath adds a directory or file to the list of allowed boundaries for read-only access.
func (sm *SecurityManager) RegisterReadOnlyPath(path string) {
	if path == "" {
		return
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	sm.readOnlyPathsMu.Lock()
	defer sm.readOnlyPathsMu.Unlock()

	// Check for duplicates
	for _, p := range sm.readOnlyPaths {
		if p == abs {
			return
		}
	}
	sm.readOnlyPaths = append(sm.readOnlyPaths, abs)
}

// GetSafePaths returns a copy of the currently registered safe paths.
func (sm *SecurityManager) GetSafePaths() []string {
	sm.safePathsMu.RLock()
	defer sm.safePathsMu.RUnlock()
	paths := make([]string, len(sm.safePaths))
	copy(paths, sm.safePaths)
	return paths
}

// GetReadOnlyPaths returns a copy of the currently registered read-only paths.
func (sm *SecurityManager) GetReadOnlyPaths() []string {
	sm.readOnlyPathsMu.RLock()
	defer sm.readOnlyPathsMu.RUnlock()
	paths := make([]string, len(sm.readOnlyPaths))
	copy(paths, sm.readOnlyPaths)
	return paths
}

// RemoveSafePath removes a path from the authorized list.
func (sm *SecurityManager) RemoveSafePath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	sm.safePathsMu.Lock()
	defer sm.safePathsMu.Unlock()

	newPaths := []string{}
	found := false
	for _, p := range sm.safePaths {
		if p == abs {
			found = true
			continue
		}
		newPaths = append(newPaths, p)
	}

	if !found {
		return fmt.Errorf("path '%s' not found in authorized list", abs)
	}

	sm.safePaths = newPaths
	return nil
}

// RemoveReadOnlyPath removes a path from the read-only authorized list.
func (sm *SecurityManager) RemoveReadOnlyPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("invalid path: %w", err)
	}

	sm.readOnlyPathsMu.Lock()
	defer sm.readOnlyPathsMu.Unlock()

	newPaths := []string{}
	found := false
	for _, p := range sm.readOnlyPaths {
		if p == abs {
			found = true
			continue
		}
		newPaths = append(newPaths, p)
	}

	if !found {
		return fmt.Errorf("path '%s' not found in read-only authorized list", abs)
	}

	sm.readOnlyPaths = newPaths
	return nil
}

// resolveSymlinks attempts to resolve all symlinks in a path. If the full path
// cannot be resolved (e.g., because the file doesn't exist yet), it attempts
// to resolve the parent directory.
func (sm *SecurityManager) resolveSymlinks(path string) string {
	// Try full path first
	if realPath, err := filepath.EvalSymlinks(path); err == nil {
		return realPath
	}
	// If it fails (likely file doesn't exist), resolve the parent directory
	dir := filepath.Dir(path)
	if realDir, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Join(realDir, filepath.Base(path))
	}
	return path // Fallback if parent also doesn't exist or other error
}

// IsPathSafe checks if a path is within the allowed boundaries (CWD, Temp, or registered Home/Config paths).
func (sm *SecurityManager) IsPathSafe(path string) error {
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
	// Use a robust resolver that handles non-existent leaf files by resolving the parent directory.
	absPath = sm.resolveSymlinks(absPath)

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
	sm.safePathsMu.RLock()
	defer sm.safePathsMu.RUnlock()

	// CRITICAL: Block access to the safePathsFile itself for the AI
	if sm.safePathsFile != "" {
		absSafeFile, err := filepath.Abs(sm.safePathsFile)
		if err == nil && absPath == absSafeFile {
			return fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	if sm.readOnlyPathsFile != "" {
		absReadSafeFile, err := filepath.Abs(sm.readOnlyPathsFile)
		if err == nil && absPath == absReadSafeFile {
			return fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
		}
	}

	for _, sp := range sm.safePaths {
		relSafe, err := filepath.Rel(sp, absPath)
		if err == nil && !strings.HasPrefix(relSafe, "..") && !filepath.IsAbs(relSafe) {
			return nil
		}
	}

	// 5. Allow paths within explicitly registered read-only paths
	sm.readOnlyPathsMu.RLock()
	defer sm.readOnlyPathsMu.RUnlock()

	for _, rop := range sm.readOnlyPaths {
		relReadSafe, err := filepath.Rel(rop, absPath)
		if err == nil && !strings.HasPrefix(relReadSafe, "..") && !filepath.IsAbs(relReadSafe) {
			return nil
		}
	}

	return fmt.Errorf("security violation: path '%s' is outside allowed boundaries (CWD, Temp, or registered paths)", path)
}

// IsPathWritable checks if a path is within the writable boundaries (CWD, Temp, or registered safe paths).
// It does NOT include read-only paths.
func (sm *SecurityManager) IsPathWritable(path string) error {
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

	// Symlink Attack Mitigation:
	absPath = sm.resolveSymlinks(absPath)

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
	sm.safePathsMu.RLock()
	defer sm.safePathsMu.RUnlock()

	if sm.safePathsFile != "" {
		absSafeFile, err := filepath.Abs(sm.safePathsFile)
		if err == nil && absPath == absSafeFile {
			return fmt.Errorf("security violation: direct access to safe paths configuration is forbidden")
		}
	}

	if sm.readOnlyPathsFile != "" {
		absReadSafeFile, err := filepath.Abs(sm.readOnlyPathsFile)
		if err == nil && absPath == absReadSafeFile {
			return fmt.Errorf("security violation: direct access to read-only paths configuration is forbidden")
		}
	}

	for _, sp := range sm.safePaths {
		relSafe, err := filepath.Rel(sp, absPath)
		if err == nil && !strings.HasPrefix(relSafe, "..") && !filepath.IsAbs(relSafe) {
			return nil
		}
	}

	return fmt.Errorf("security violation: path '%s' is not in a writable boundary (read-only or unregistered)", path)
}
