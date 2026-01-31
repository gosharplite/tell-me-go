// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/registry"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type PolicyTool struct {
	sm *security.SecurityManager
}

// NewPolicyTool creates a new PolicyTool.
func NewPolicyTool(sm *security.SecurityManager) *PolicyTool {
	return &PolicyTool{sm: sm}
}

func (t *PolicyTool) RegisterSafePath(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	reason := params.Reason

	if path == "" {
		return types.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// 1. Confirmation
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Authorization auto-approved.\033[0m\n")
		// t.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path? (y/N) ")

		char, err := t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read/write files in this location in future sessions.\033[0m (y/N) ")
		char, err = t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		// t.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	t.sm.RegisterSafePath(absPath)
	if err := t.sm.SaveSafePaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path authorized but failed to persist: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized and persisted.", absPath)}, nil
}

func (t *PolicyTool) RemoveSafePath(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		return types.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation Gate
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of authorization auto-approved.\033[0m\n")
		// t.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		// t.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := t.sm.RemoveSafePath(absPath); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.SaveSafePaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from authorized boundaries.", absPath)}, nil
}

func (t *PolicyTool) ListSafePaths(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	paths := t.sm.GetSafePaths()
	if len(paths) == 0 {
		return types.ToolResult{Text: "No additional safe paths are currently registered."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized safe paths:\n")
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("- %s\n", p))
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func (t *PolicyTool) RegisterReadPath(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	reason := params.Reason

	if path == "" {
		return types.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// 1. Confirmation
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Read-only authorization auto-approved.\033[0m\n")
		// t.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent READ-ONLY access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path for reading? (y/N) ")

		char, err := t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read files in this location in future sessions.\033[0m (y/N) ")
		char, err = t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		// t.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	t.sm.RegisterReadOnlyPath(absPath)
	if err := t.sm.SaveReadOnlyPaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path authorized for reading but failed to persist: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized for reading and persisted.", absPath)}, nil
}

func (t *PolicyTool) RemoveReadPath(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path string `json:"path"`
	}
	if err := registry.UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	path := params.Path
	if path == "" {
		return types.ToolResult{}, fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("invalid path: %w", err)
	}

	// Confirmation Gate
	if t.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of read-only authorization auto-approved.\033[0m\n")
		// t.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE read-only authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := t.sm.ReadSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		// t.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := t.sm.RemoveReadOnlyPath(absPath); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := t.sm.SaveReadOnlyPaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath)}, nil
}

func (t *PolicyTool) ListReadPaths(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	paths := t.sm.GetReadOnlyPaths()
	if len(paths) == 0 {
		return types.ToolResult{Text: "No additional read-only paths are currently registered."}, nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized read-only paths:\n")
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("- %s\n", p))
	}
	return types.ToolResult{Text: sb.String()}, nil
}

func (t *PolicyTool) BypassConfirmation(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	if t.sm.IsBypassActive() {
		return types.ToolResult{Text: "Bypass mode is already enabled."}, nil
	}

	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting to DISABLE ALL interactive security prompts.\033[0m\n")
	fmt.Fprintf(os.Stderr, "This allows the AI to execute commands and write files without further confirmation.\n")
	fmt.Fprintf(os.Stderr, "Enable bypass mode for this run? (y/N) ")

	char, err := t.sm.ReadSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return types.ToolResult{}, err
	}
	if char != "y" {
		return types.ToolResult{Text: "Bypass mode denied by user."}, nil
	}

	t.sm.SetBypassActive(true)

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.\033[0m\n")
	// t.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return types.ToolResult{Text: "All future confirmations in this session will be bypassed. This setting is now persistent for this session name."}, nil
}

func (t *PolicyTool) RevokeBypass(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.SetBypassActive(false)

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;32m[SECURITY] Interactive security prompts have been RE-ENABLED.\033[0m\n")
	// t.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return types.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}

// RegisterPolicy adds security policy management tools to the registry.
func RegisterPolicy(r *registry.Registry, sm *security.SecurityManager) {
	p := NewPolicyTool(sm)

	r.Register(&types.ToolDeclaration{
		Name:        "register_safepath",
		Description: "Adds a path to the persistent 'safe' list, allowing future AI sessions to read/write in that location without repeating security authorizations.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
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
	}, p.RegisterSafePath)

	r.Register(&types.ToolDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, p.ListSafePaths)

	r.Register(&types.ToolDeclaration{
		Name:        "remove_safepath",
		Description: "Removes a directory or file from the authorized boundaries.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The path to remove from authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, p.RemoveSafePath)

	r.Register(&types.ToolDeclaration{
		Name:        "register_readpath",
		Description: "Adds a directory or file to the allowed boundaries for READ-ONLY access. This is a persistent configuration.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
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
	}, p.RegisterReadPath)

	r.Register(&types.ToolDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, p.ListReadPaths)

	r.Register(&types.ToolDeclaration{
		Name:        "remove_readpath",
		Description: "Removes a directory or file from the read-only authorized boundaries.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"path": {
					Type:        "STRING",
					Description: "The path to remove from read-only authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, p.RemoveReadPath)

	r.Register(&types.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, p.BypassConfirmation)

	r.Register(&types.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, p.RevokeBypass)
}
