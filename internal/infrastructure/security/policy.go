// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

type policyTool struct {
	sm *SecurityManager
}

// newPolicyTool creates a new policyTool.
func newPolicyTool(sm *SecurityManager) *policyTool {
	return &policyTool{sm: sm}
}

func (t *policyTool) RegisterSafePath(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
	confirmed, err := t.confirmAction(ctx, "persistent access to:", absPath, reason, true)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Access denied by user."}, nil
	}

	// Register and Persist
	t.sm.RegisterSafePath(absPath)
	if err := t.sm.saveSafePaths(ctx); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path authorized but failed to persist: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized and persisted.", absPath)}, nil
}

func (t *policyTool) RemoveSafePath(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
	confirmed, err := t.confirmAction(ctx, "to REMOVE authorization for:", absPath, "", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Removal denied by user."}, nil
	}

	if err := t.sm.removeSafePath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.saveSafePaths(ctx); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from authorized boundaries.", absPath)}, nil
}

func (t *policyTool) ListSafePaths(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

func (t *policyTool) RegisterReadPath(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
	confirmed, err := t.confirmAction(ctx, "persistent READ-ONLY access to:", absPath, reason, true)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Access denied by user."}, nil
	}

	// Register and Persist
	t.sm.RegisterReadOnlyPath(absPath)
	if err := t.sm.saveReadOnlyPaths(ctx); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path authorized for reading but failed to persist: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized for reading and persisted.", absPath)}, nil
}

func (t *policyTool) RemoveReadPath(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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
	confirmed, err := t.confirmAction(ctx, "to REMOVE read-only authorization for:", absPath, "", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Removal denied by user."}, nil
	}

	if err := t.sm.removeReadOnlyPath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.saveReadOnlyPaths(ctx); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath)}, nil
}

func (t *policyTool) ListReadPaths(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
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

func (t *policyTool) BypassConfirmation(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	if t.sm.IsBypassActive() {
		return tools.ToolResult{Text: "Bypass mode is already enabled."}, nil
	}

	// Confirmation
	confirmed, err := t.confirmAction(ctx, "to DISABLE ALL interactive security prompts.", "", "This allows the AI to execute commands and write files without further confirmation.", false)
	if err != nil {
		return tools.ToolResult{}, err
	}
	if !confirmed {
		return tools.ToolResult{Text: "Bypass mode denied by user."}, nil
	}

	t.sm.SetBypassActive(true)

	t.sm.saveBypassState(ctx)
	t.sm.Warn("[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.")
	// t.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return tools.ToolResult{Text: "All future confirmations in this session will be bypassed. This setting is now persistent for this session name."}, nil
}

func (t *policyTool) RevokeBypass(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.SetBypassActive(false)

	t.sm.saveBypassState(ctx)
	t.sm.Warn("[SECURITY] Interactive security prompts have been RE-ENABLED.")
	// t.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return tools.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}

func (t *policyTool) confirmAction(ctx context.Context, title, path, reason string, doubleConfirm bool) (bool, error) {
	lowerTitle := strings.ToLower(title)
	if t.sm.IsBypassActive() {
		t.sm.Warn(fmt.Sprintf("[Bypassed] %s auto-approved.", t.getBypassMsg(lowerTitle)))
		return true, nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "[SECURITY] AI is requesting %s %s\n", title, path)
	if reason != "" {
		if path == "" {
			sb.WriteString(reason + "\n")
		} else {
			fmt.Fprintf(&sb, "Reason: %s\n", reason)
		}
	}

	sb.WriteString(t.getPrompt(lowerTitle))
	confirmed, err := t.sm.interaction.interactor.Confirm(ctx, sb.String())
	if err != nil || !confirmed {
		return false, err
	}

	if doubleConfirm {
		confirmed, err = t.sm.interaction.interactor.Confirm(ctx, fmt.Sprintf("[DOUBLE CONFIRM] %s (y/N) ", t.getDoubleMsg(lowerTitle)))
		if err != nil || !confirmed {
			return false, err
		}
	}

	return true, nil
}

func (t *policyTool) getBypassMsg(lowerTitle string) string {
	if strings.Contains(lowerTitle, "remove") {
		if strings.Contains(lowerTitle, "read-only") {
			return "Removal of read-only authorization"
		}
		return "Removal of authorization"
	}
	if strings.Contains(lowerTitle, "read-only") {
		return "Read-only authorization"
	}
	return "Authorization"
}

func (t *policyTool) getPrompt(lowerTitle string) string {
	if strings.Contains(lowerTitle, "remove") {
		return "Confirm removal? (y/N) "
	}
	if strings.Contains(lowerTitle, "read-only") {
		return "Authorize this path for reading? (y/N) "
	}
	if strings.Contains(lowerTitle, "disable all") {
		return "Enable bypass mode for this run? (y/N) "
	}
	return "Authorize this path? (y/N) "
}

func (t *policyTool) getDoubleMsg(lowerTitle string) string {
	if strings.Contains(lowerTitle, "read-only") {
		return "Are you absolutely sure? This allows the AI to read files in this location in future sessions."
	}
	return "Are you absolutely sure? This allows the AI to read/write files in this location in future sessions."
}

// RegisterPolicyTools adds security policy management tools to the registry.
func (sm *SecurityManager) RegisterPolicyTools(r tools.IToolRegistry) error {
	p := newPolicyTool(sm)

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RegisterSafePath, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, p.ListSafePaths); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RemoveSafePath, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RegisterReadPath, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.Register(&tools.ToolDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, p.ListReadPaths); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RemoveReadPath, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, p.BypassConfirmation, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	if err := r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, p.RevokeBypass, tools.ToolOptions{Serial: true, LongRunning: true}); err != nil {
		return err
	}

	return nil
}
