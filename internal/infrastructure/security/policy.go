// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/registry"
	"github.com/gosharplite/tell-me-go/internal/ui"
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := t.sm.SaveSafePaths(ctx); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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

	if err := t.sm.RemoveSafePath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.SaveSafePaths(ctx); err != nil {
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
		sb.WriteString(fmt.Sprintf("- %s\n", p))
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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
	if err := t.sm.SaveReadOnlyPaths(ctx); err != nil {
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
	if err := registry.UnmarshalArgs(args, &params); err != nil {
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

	if err := t.sm.RemoveReadOnlyPath(absPath); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.SaveReadOnlyPaths(ctx); err != nil {
		return tools.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return tools.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath)}, nil
}

func (t *policyTool) ListReadPaths(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	paths := t.sm.GetReadOnlyPaths()
	if len(paths) == 0 {
		return tools.ToolResult{Text: "No additional read-only paths are currently registered."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized read-only paths:\n")
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("- %s\n", p))
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

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "%s[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.%s\n", ui.ColorBoldRed, ui.ColorReset)
	// t.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return tools.ToolResult{Text: "All future confirmations in this session will be bypassed. This setting is now persistent for this session name."}, nil
}

func (t *policyTool) RevokeBypass(ctx context.Context, args map[string]interface{}) (tools.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.SetBypassActive(false)

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "%s[SECURITY] Interactive security prompts have been RE-ENABLED.%s\n", ui.ColorBoldGreen, ui.ColorReset)
	// t.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return tools.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}

func (t *policyTool) confirmAction(ctx context.Context, title, path, reason string, doubleConfirm bool) (bool, error) {
	lowerTitle := strings.ToLower(title)
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "%s[Bypassed] %s auto-approved.%s\n", ui.ColorGreen, t.getBypassMsg(lowerTitle), ui.ColorReset)
		return true, nil
	}

	mainColor := ui.ColorBoldRed
	if strings.Contains(lowerTitle, "remove") {
		mainColor = ui.ColorBoldYellow
	}

	fmt.Fprintf(os.Stderr, "%s[SECURITY] AI is requesting %s%s %s\n", mainColor, title, ui.ColorReset, path)
	if reason != "" {
		if path == "" {
			fmt.Fprintf(os.Stderr, "%s\n", reason)
		} else {
			fmt.Fprintf(os.Stderr, "%sReason: %s%s\n", ui.ColorYellow, reason, ui.ColorReset)
		}
	}

	fmt.Fprintf(os.Stderr, "%s", t.getPrompt(lowerTitle))
	char, err := t.sm.ReadSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil || char != "y" {
		return false, err
	}

	if doubleConfirm {
		fmt.Fprintf(os.Stderr, "%s[DOUBLE CONFIRM] %s%s (y/N) ", ui.ColorBoldRed, t.getDoubleMsg(lowerTitle), ui.ColorReset)
		char, err = t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
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

// RegisterPolicy adds security policy management tools to the registry.
func RegisterPolicy(r *registry.Registry, sm *SecurityManager) {
	p := newPolicyTool(sm)

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RegisterSafePath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, p.ListSafePaths)

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RemoveSafePath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RegisterReadPath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.Register(&tools.ToolDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, p.ListReadPaths)

	r.RegisterWithOptions(&tools.ToolDeclaration{
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
	}, p.RemoveReadPath, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, p.BypassConfirmation, registry.ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&tools.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, p.RevokeBypass, registry.ToolOptions{Serial: true, LongRunning: true})
}
