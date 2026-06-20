// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

// actionType identifies the security action being confirmed.
type actionType int

const (
	actionPathWrite      actionType = iota // "persistent access to:"
	actionPathRemove                       // "to REMOVE authorization for:"
	actionPathRead                         // "persistent READ-ONLY access to:"
	actionPathRemoveRead                   // "to REMOVE read-only authorization for:"
	actionBypassEnable                     // "to DISABLE ALL interactive security prompts."
	actionSessionUpdate                    // "to update session setting:"
)

// actionTitle maps an actionType to its human-readable title string.
func actionTitle(action actionType) string {
	switch action {
	case actionPathWrite:
		return "persistent access to:"
	case actionPathRemove:
		return "to REMOVE authorization for:"
	case actionPathRead:
		return "persistent READ-ONLY access to:"
	case actionPathRemoveRead:
		return "to REMOVE read-only authorization for:"
	case actionBypassEnable:
		return "to DISABLE ALL interactive security prompts."
	case actionSessionUpdate:
		return "to update session setting:"
	default:
		return ""
	}
}

type policyTool struct {
	sm *SecurityManager
	kv ports.KVStore
}

// newPolicyTool creates a new policyTool.
func newPolicyTool(sm *SecurityManager, kv ports.KVStore) (*policyTool, error) {
	if sm == nil {
		return nil, fmt.Errorf("SecurityManager dependency is required")
	}
	if kv == nil {
		return nil, fmt.Errorf("KVStore dependency is required")
	}
	return &policyTool{sm: sm, kv: kv}, nil
}

func (t *policyTool) RegisterSafePath(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	reason := params.Reason

	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionPathWrite, absPath, reason, true)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Access denied by user."}, nil
	}

	// Register and Persist
	t.sm.RegisterSafePath(absPath)
	if err := t.persistPaths(ctx, true); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path authorized but failed to persist: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized and persisted.", absPath)}, nil
}

func (t *policyTool) RemoveSafePath(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionPathRemove, absPath, "", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Removal denied by user."}, nil
	}

	if err := t.sm.removeSafePath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.persistPaths(ctx, true); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from authorized boundaries.", absPath)}, nil
}

func (t *policyTool) ListSafePaths(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	paths := t.sm.GetSafePaths()
	if len(paths) == 0 {
		return tools.ToolResult{Text: "No additional safe paths are currently registered."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized safe paths:\n")
	for _, p := range paths {
		fmt.Fprintf(&sb, "- %s\n", p)
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *policyTool) RegisterReadPath(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	reason := params.Reason

	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionPathRead, absPath, reason, true)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Access denied by user."}, nil
	}

	// Register and Persist
	t.sm.RegisterReadOnlyPath(absPath)
	if err := t.persistPaths(ctx, false); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path authorized for reading but failed to persist: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized for reading and persisted.", absPath)}, nil
}

func (t *policyTool) RemoveReadPath(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path string `json:"path"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		return tools.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionPathRemoveRead, absPath, "", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Removal denied by user."}, nil
	}

	if err := t.sm.removeReadOnlyPath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.persistPaths(ctx, false); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath)}, nil
}

func (t *policyTool) ListReadPaths(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	paths := t.sm.getReadOnlyPaths()
	if len(paths) == 0 {
		return tools.ToolResult{Text: "No additional read-only paths are currently registered."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized read-only paths:\n")
	for _, p := range paths {
		fmt.Fprintf(&sb, "- %s\n", p)
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *policyTool) persistPaths(ctx context.Context, safe bool) error {
	var key string
	var paths []string
	if safe {
		key = "authorized_safe_paths"
		paths = t.sm.GetSafePaths()
	} else {
		key = "authorized_read_paths"
		paths = t.sm.getReadOnlyPaths()
	}

	data, err := json.Marshal(paths)
	if err != nil {
		return fmt.Errorf("failed to marshal paths: %w", err)
	}

	return t.kv.Set(ctx, key, string(data))
}

func (t *policyTool) BypassConfirmation(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	if t.sm.IsBypassActive() {
		return tools.ToolResult{Text: "Bypass mode is already enabled."}, nil
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionBypassEnable, "", "This allows the AI to execute commands and write files without further confirmation.", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Bypass mode denied by user."}, nil
	}

	t.sm.SetBypassActive(true)

	if err := t.kv.Set(ctx, "bypass_confirmation", "true"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to persist bypass status: %w", err)
	}

	t.sm.Warn("[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS MODE.")
	// t.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this mode.")
	return tools.ToolResult{Text: "All future confirmations for this mode will be bypassed. This setting is now **persistent across session rotations** until manually revoked."}, nil
}

func (t *policyTool) RevokeBypass(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.SetBypassActive(false)

	if err := t.kv.Set(ctx, "bypass_confirmation", "false"); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to persist bypass revocation: %w", err)
	}

	t.sm.Warn("[SECURITY] Interactive security prompts have been RE-ENABLED.")
	// t.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return tools.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}

func (t *policyTool) UpdateSessionSetting(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := tools.UnmarshalArgs(args, &params); err != nil {
		return tools.ToolResult{}, err
	}

	if params.Key == "" {
		return tools.ToolResult{}, fmt.Errorf("key argument is required")
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, actionSessionUpdate, params.Key, fmt.Sprintf("New Value: %s", params.Value), false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Update denied by user."}, nil
	}

	if err := t.kv.Set(ctx, params.Key, params.Value); err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to update setting: %w", err)
	}

	return tools.ToolResult{Text: fmt.Sprintf("Session setting '%s' has been updated to '%s'. This change is persistent across sessions.", params.Key, params.Value)}, nil
}

func (t *policyTool) ListSessionSettings(ctx context.Context, args map[string]interface{}, hb chan<- struct{}) (tools.ToolResult, error) {
	settings, err := t.kv.GetAll(ctx)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to retrieve settings: %w", err)
	}

	if len(settings) == 0 {
		return tools.ToolResult{Text: "No persistent session settings are currently defined."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Current persistent session settings:\n")
	sb.WriteString("| Key | Value |\n")
	sb.WriteString("| :--- | :--- |\n")
	for k, v := range settings {
		fmt.Fprintf(&sb, "| %s | %s |\n", k, v)
	}
	return tools.ToolResult{Text: sb.String()}, nil
}

func (t *policyTool) confirmAction(ctx context.Context, action actionType, path, reason string, doubleConfirm bool) (bool, error) {
	if t.sm.IsBypassActive() {
		t.sm.Warn(fmt.Sprintf("[Bypassed] %s auto-approved.", t.getBypassMsg(action)))
		return true, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[SECURITY] AI is requesting %s %s\n", actionTitle(action), path)
	t.writeConfirmReason(&sb, reason, path)

	sb.WriteString(t.getPrompt(action))
	confirmed, err := t.sm.interaction.interactorProvider().Confirm(ctx, sb.String())
	if err != nil || !confirmed {
		return false, err
	}

	if doubleConfirm {
		return t.confirmDouble(ctx, action)
	}

	return true, nil
}

// writeConfirmReason appends the reason line to the confirmation message builder.
func (t *policyTool) writeConfirmReason(sb *strings.Builder, reason, path string) {
	if reason == "" {
		return
	}
	if path == "" {
		sb.WriteString(reason + "\n")
	} else {
		fmt.Fprintf(sb, "Reason: %s\n", reason)
	}
}

// confirmDouble performs the second ("double") confirmation step.
func (t *policyTool) confirmDouble(ctx context.Context, action actionType) (bool, error) {
	confirmed, err := t.sm.interaction.interactorProvider().Confirm(ctx,
		fmt.Sprintf("[DOUBLE CONFIRM] %s (y/N) ", t.getDoubleMsg(action)))
	if err != nil || !confirmed {
		return false, err
	}
	return true, nil
}

func (t *policyTool) getBypassMsg(action actionType) string {
	switch action {
	case actionPathRemove:
		return "Removal of authorization"
	case actionPathRemoveRead:
		return "Removal of read-only authorization"
	case actionPathRead:
		return "Read-only authorization"
	default:
		return "Authorization"
	}
}

func (t *policyTool) getPrompt(action actionType) string {
	switch action {
	case actionPathRemove, actionPathRemoveRead:
		return "Confirm removal? (y/N) "
	case actionPathRead:
		return "Authorize this path for reading? (y/N) "
	case actionBypassEnable:
		return "Enable bypass mode for this run? (y/N) "
	default:
		return "Authorize this path? (y/N) "
	}
}

func (t *policyTool) getDoubleMsg(action actionType) string {
	switch action {
	case actionPathRead, actionPathRemoveRead:
		return "Are you absolutely sure? This allows the AI to read files in this location in future sessions."
	default:
		return "Are you absolutely sure? This allows the AI to read/write files in this location in future sessions."
	}
}

type toolEntry struct {
	decl    *tools.ToolDeclaration
	handler tools.ToolFunc
	opts    *tools.ToolOptions // nil if using standard Register
}

// RegisterPolicyTools adds security policy management tools to the registry.
func (sm *SecurityManager) RegisterPolicyTools(r tools.Registry, kv ports.KVStore) error {
	p, err := newPolicyTool(sm, kv)
	if err != nil {
		return err
	}

	for _, e := range getPolicyToolEntries(p) {
		var err error
		if e.opts != nil {
			err = r.RegisterWithOptions(e.decl, e.handler, *e.opts)
		} else {
			err = r.Register(e.decl, e.handler)
		}
		if err != nil {
			return err
		}
	}

	return nil
}

func getPolicyToolEntries(p *policyTool) []toolEntry {
	return []toolEntry{
		{
			decl: &tools.ToolDeclaration{
				Name:        "register_safepath",
				Description: "Adds a path to the persistent 'safe' list, allowing future AI sessions to read/write in that location without repeating security authorizations.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {
							Type:        "STRING",
							Description: "The absolute or relative path to authorize.",
						},
						"reason": {
							Type:        "STRING",
							Description: "Reason why this path needs to be authorized.",
						},
					},
					Required: []string{"path", "reason"},
				},
			},
			handler: p.RegisterSafePath,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_safepaths",
				Description: "Lists all currently authorized safe paths and files.",
			},
			handler: p.ListSafePaths,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "remove_safepath",
				Description: "Removes a directory or file from the authorized boundaries.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {
							Type:        "STRING",
							Description: "The path to remove from authorized boundaries.",
						},
					},
					Required: []string{"path"},
				},
			},
			handler: p.RemoveSafePath,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "register_readpath",
				Description: "Adds a directory or file to the allowed boundaries for READ-ONLY access. This is a persistent configuration.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {
							Type:        "STRING",
							Description: "The absolute or relative path to authorize for reading.",
						},
						"reason": {
							Type:        "STRING",
							Description: "Reason why this path needs to be authorized.",
						},
					},
					Required: []string{"path", "reason"},
				},
			},
			handler: p.RegisterReadPath,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_readpaths",
				Description: "Lists all currently authorized read-only paths and files.",
			},
			handler: p.ListReadPaths,
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "remove_readpath",
				Description: "Removes a directory or file from the read-only authorized boundaries.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"path": {
							Type:        "STRING",
							Description: "The path to remove from read-only authorized boundaries.",
						},
					},
					Required: []string{"path"},
				},
			},
			handler: p.RemoveReadPath,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "bypass_confirmation",
				Description: "Disables all interactive security prompts for the current mode. This setting is **persistent across sessions** and remains active until manually revoked.",
			},
			handler: p.BypassConfirmation,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "revoke_bypass",
				Description: "Re-enables interactive security prompts by revoking the bypass status.",
			},
			handler: p.RevokeBypass,
			opts:    &tools.ToolOptions{Serial: true, LongRunning: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "update_session_setting",
				Description: "Updates a persistent session configuration setting. These settings persist across session rotations and system restarts.",
				Parameters: &tools.Schema{
					Type: "OBJECT",
					Properties: map[string]*tools.Schema{
						"key": {
							Type:        "STRING",
							Description: "The name of the setting to update (e.g., 'backup_retention_days', 'bypass_confirmation').",
						},
						"value": {
							Type:        "STRING",
							Description: "The new value for the setting.",
						},
					},
					Required: []string{"key", "value"},
				},
			},
			handler: p.UpdateSessionSetting,
			opts:    &tools.ToolOptions{Serial: true},
		},
		{
			decl: &tools.ToolDeclaration{
				Name:        "list_session_settings",
				Description: "Lists all current persistent session settings and their values.",
			},
			handler: p.ListSessionSettings,
		},
	}
}
