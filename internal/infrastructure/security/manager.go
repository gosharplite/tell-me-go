// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// SecurityManager coordinates path validation, user interaction, and auditing.
type SecurityManager struct {
	Policy      *PathPolicy
	Interaction *InteractionHandler
	Auditor     AuditLogger

	bypassFile   string
	bypassActive bool
	bypassMu     sync.RWMutex
}

// NewSecurityManager creates a new SecurityManager.
func NewSecurityManager(input io.Reader) *SecurityManager {
	auditor := NewAuditor()
	return &SecurityManager{
		Policy:      NewPathPolicy(),
		Interaction: NewInteractionHandler(input, auditor),
		Auditor:     auditor,
	}
}

// IsPathSafe checks if a path is safe.
func (sm *SecurityManager) IsPathSafe(path string) (string, error) {
	return sm.Policy.ValidatePath(path, false)
}

// IsPathWritable checks if a path is writable.
func (sm *SecurityManager) IsPathWritable(path string) (string, error) {
	return sm.Policy.ValidatePath(path, true)
}

// ConfirmDestructiveAction prompts the user for confirmation.
func (sm *SecurityManager) ConfirmDestructiveAction(ctx context.Context, action, target, detail string) (bool, error) {
	return sm.Interaction.ConfirmAction(ctx, action, target, detail, sm.IsBypassActive())
}

// Authorize prompts the user for authorization of a specific command or action.
func (sm *SecurityManager) Authorize(ctx context.Context, label, detail, reason string, isSafe bool) (bool, error) {
	if sm.IsBypassActive() {
		return true, nil
	}
	if isSafe {
		return true, nil
	}
	return sm.Interaction.ConfirmAction(ctx, "Execute "+label, detail, reason, false)
}

// LogAudit writes an audit entry.
func (sm *SecurityManager) LogAudit(label1, val1, label2, val2 string) {
	sm.Auditor.LogAudit(label1, val1, label2, val2)
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
		sm.bypassActive = strings.TrimSpace(string(data)) == "true"
	}
}

// IsBypassActive returns the current state of bypass_confirmation.
func (sm *SecurityManager) IsBypassActive() bool {
	sm.bypassMu.RLock()
	defer sm.bypassMu.RUnlock()
	return sm.bypassActive
}

// SaveBypassState writes the persistent bypass state to disk.
func (sm *SecurityManager) SaveBypassState(ctx context.Context) {
	sm.bypassMu.RLock()
	file := sm.bypassFile
	active := sm.bypassActive
	sm.bypassMu.RUnlock()

	if file == "" {
		return
	}
	val := "false"
	if active {
		val = "true"
	}
	_ = storage.AtomicWrite(ctx, file, []byte(val), 0644)
}

// SetBypassActive sets the bypass state.
func (sm *SecurityManager) SetBypassActive(active bool) {
	sm.bypassMu.Lock()
	defer sm.bypassMu.Unlock()
	sm.bypassActive = active
}

var allowedCommands = map[string]bool{
	// Shell commands (for execute_command/pipe_commands)
	"go":            true,
	"git":           true,
	"ls":            true,
	"grep":          true,
	"cat":           true,
	"diff":          true,
	"whoami":        true,
	"stat":          true,
	"find":          true,
	"sh":            true,
	"make":          true,
	"npm":           true,
	"node":          true,
	"cargo":         true,
	"pytest":        true,
	"python":        true,
	"python3":       true,
	"pwd":           true,
	"echo":          true,
	"head":          true,
	"tail":          true,
	"wc":            true,
	"date":          true,
	"golangci-lint": true,
	"staticcheck":   true,
	"govulncheck":   true,
	"cp":            true,
	"mv":            true,
	"rm":            true,
	"mkdir":         true,
	"touch":         true,
	"chmod":         true,
	"chown":         true,
	"tar":           true,
	"zip":           true,
	"unzip":         true,
	"curl":          true,
	"wget":          true,

	// Filesystem Tools
	"list_files":        true,
	"get_tree":          true,
	"read_file":         true,
	"search_files":      true,
	"replace_text":      true,
	"find_file":         true,
	"write_file":        true,
	"append_text":       true,
	"get_file_diff":     true,
	"undo_file_change":  true,
	"register_safepath": true,
	"list_safepaths":    true,
	"remove_safepath":   true,
	"register_readpath": true,
	"list_readpaths":    true,
	"remove_readpath":   true,

	// Code Analysis Tools
	"get_definitions":          true,
	"get_file_skeleton":        true,
	"verify_architecture":      true,
	"get_code_health":          true,
	"find_usages":              true,
	"find_definitions":         true,
	"list_symbols":             true,
	"list_implementations":     true,
	"get_type_info":            true,
	"get_project_summary":      true,
	"search_usages_globally":   true,
	"get_semantic_diff":        true,
	"rename_symbol":            true,
	"list_todos":               true,
	"go_doc":                   true,
	"get_complexity_metrics":   true,
	"get_package_graph":        true,
	"analyze_sequence_flow":    true,
	"get_detailed_coverage":    true,
	"dead_code_graph":          true,
	"generate_mermaid_diagram": true,
	"move_definition":          true,
	"check_vulnerabilities":    true,
	"run_linter":               true,

	// Development Tools
	"execute_command": true,
	"pipe_commands":   true,
	"run_tests":       true,
	"go_tidy":         true,
	"get_coverage":    true,
	"run_benchmark":   true,

	// Git Tools
	"get_git_status":    true,
	"get_git_diff":      true,
	"get_git_log":       true,
	"get_git_show":      true,
	"get_git_blame":     true,
	"git_commit":        true,
	"git_create_branch": true,

	// Communication & External Tools
	"send_teams_message": true,
	"read_external_docs": true,
	"http_request":       true,

	// Session & Management Tools
	"get_session_info":         true,
	"manage_scratchpad":        true,
	"manage_config":            true,
	"manage_tasks":             true,
	"ask_user":                 true,
	"bypass_confirmation":      true,
	"revoke_bypass":            true,
	"estimate_cost":            true,
	"get_cost_summary":         true,
	"verify_release_readiness": true,
	"summarize_history":        true,
	"manage_history":           true,

	// Media Tools
	"create_image": true,
	"read_image":   true,
}

// IsCommandAllowed checks if a base command is allowed for execution.
func (sm *SecurityManager) IsCommandAllowed(command string) bool {
	return allowedCommands[command]
}

// TerminalLock locks the terminal.
func (sm *SecurityManager) TerminalLock() {
	sm.Interaction.TerminalLock()
}

// TerminalUnlock unlocks the terminal.
func (sm *SecurityManager) TerminalUnlock() {
	sm.Interaction.TerminalUnlock()
}

// ReadSingleKey reads a single key.
func (sm *SecurityManager) ReadSingleKey(ctx context.Context) (string, error) {
	return sm.Interaction.ReadSingleKey(ctx)
}

// ReadLine reads a line.
func (sm *SecurityManager) ReadLine(ctx context.Context) (string, error) {
	return sm.Interaction.ReadLine(ctx)
}

// SetInputReader sets the input reader.
func (sm *SecurityManager) SetInputReader(r io.Reader) {
	sm.Interaction.SetReader(r)
}

// RegisterSafePath registers a safe path.
func (sm *SecurityManager) RegisterSafePath(path string) {
	sm.Policy.RegisterPath(path, true)
}

// SaveSafePaths saves safe paths.
func (sm *SecurityManager) SaveSafePaths(ctx context.Context) error {
	return sm.Policy.SavePaths(ctx, true)
}

// RemoveSafePath removes a safe path.
func (sm *SecurityManager) RemoveSafePath(path string) error {
	return sm.Policy.RemovePath(path, true)
}

// SetCommandsLogFile sets the commands log file.
func (sm *SecurityManager) SetCommandsLogFile(path string) {
	sm.Auditor.SetLogFile(path)
}

// SetSafePathsFile sets the safe paths file.
func (sm *SecurityManager) SetSafePathsFile(path string) {
	sm.Policy.SetConfigFile(path, true)
}

// SetReadOnlyPathsFile sets the read-only paths file.
func (sm *SecurityManager) SetReadOnlyPathsFile(path string) {
	sm.Policy.SetConfigFile(path, false)
}

// LoadSafePaths loads safe paths.
func (sm *SecurityManager) LoadSafePaths() error {
	return sm.Policy.LoadPaths(true)
}

// LoadReadOnlyPaths loads read-only paths.
func (sm *SecurityManager) LoadReadOnlyPaths() error {
	return sm.Policy.LoadPaths(false)
}

// SaveReadOnlyPaths saves read-only paths.
func (sm *SecurityManager) SaveReadOnlyPaths(ctx context.Context) error {
	return sm.Policy.SavePaths(ctx, false)
}

// RegisterReadOnlyPath registers a read-only path.
func (sm *SecurityManager) RegisterReadOnlyPath(path string) {
	sm.Policy.RegisterPath(path, false)
}

// GetSafePaths returns safe paths.
func (sm *SecurityManager) GetSafePaths() []string {
	return sm.Policy.GetPaths(true)
}

// GetReadOnlyPaths returns read-only paths.
func (sm *SecurityManager) GetReadOnlyPaths() []string {
	return sm.Policy.GetPaths(false)
}

// RemoveReadOnlyPath removes a read-only path.
func (sm *SecurityManager) RemoveReadOnlyPath(path string) error {
	return sm.Policy.RemovePath(path, false)
}
