// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gosharplite/tell-me-go/internal/types"
)

type PolicyTool struct {
	sm *SecurityManager
}

func (t *PolicyTool) RegisterSafePath(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	var params struct {
		Path   string `json:"path"`
		Reason string `json:"reason"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
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
		t.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read/write files in this location in future sessions.\033[0m (y/N) ")
		char, err = t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		t.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
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
	if err := UnmarshalArgs(args, &params); err != nil {
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
		t.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		t.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "User manually approved")
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
	if err := UnmarshalArgs(args, &params); err != nil {
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
		t.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent READ-ONLY access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path for reading? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read files in this location in future sessions.\033[0m (y/N) ")
		char, err = t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		t.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
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
	if err := UnmarshalArgs(args, &params); err != nil {
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
		t.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE read-only authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := t.sm.readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		t.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "User manually approved")
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

	char, err := t.sm.readSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return types.ToolResult{}, err
	}
	if char != "y" {
		return types.ToolResult{Text: "Bypass mode denied by user."}, nil
	}

	t.sm.bypassMu.Lock()
	t.sm.bypassConfirmations = true
	t.sm.bypassMu.Unlock()

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.\033[0m\n")
	t.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return types.ToolResult{Text: "All future confirmations in this session will be bypassed. This setting is now persistent for this session name."}, nil
}

func (t *PolicyTool) RevokeBypass(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	t.sm.TerminalLock()
	defer t.sm.TerminalUnlock()

	t.sm.bypassMu.Lock()
	t.sm.bypassConfirmations = false
	t.sm.bypassMu.Unlock()

	t.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;32m[SECURITY] Interactive security prompts have been RE-ENABLED.\033[0m\n")
	t.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return types.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}
