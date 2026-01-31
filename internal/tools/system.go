// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/shlex"
	"github.com/gosharplite/tell-me-go/internal/types"
)

type systemManager struct {
	sm *SecurityManager
}

// RegisterSystemTools adds system-related tools to the registry.
func RegisterSystemTools(r *Registry, sm *SecurityManager) {
	m := &systemManager{sm: sm}

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "execute_command",
		Description: "Executes a single shell command without shell interpretation (direct binary call). Security: Only whitelisted commands are auto-approved; others require user confirmation.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"command": {
					Type:        "STRING",
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this command needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"command"},
		},
	}, m.executeCommand, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "pipe_commands",
		Description: "Executes a sequence of commands by piping the output of each to the next. Security: All commands in the pipe must be whitelisted for auto-approval.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"commands": {
					Type: "ARRAY",
					Items: &types.Schema{
						Type: "STRING",
					},
					Description: "The sequence of commands to pipe (e.g., ['ls -la', 'grep .go']).",
				},
				"reason": {
					Type:        "STRING",
					Description: "A short explanation of why this pipeline needs to be executed.",
				},
				"output_file": {
					Type:        "STRING",
					Description: "Optional: Redirect the final output to this file.",
				},
				"append": {
					Type:        "BOOLEAN",
					Description: "Optional: If output_file is set, append to it instead of overwriting.",
				},
			},
			Required: []string{"commands"},
		},
	}, m.pipeCommands, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "ask_user",
		Description: "Asks the user a specific question to clarify requirements or request confirmation.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"question": {
					Type:        "STRING",
					Description: "The question to ask the user.",
				},
			},
			Required: []string{"question"},
		},
	}, m.askUser, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches and cleans content from a URL, stripping HTML tags and scripts to provide readable documentation. Useful for researching library APIs.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"url": {
					Type:        "STRING",
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, m.readExternalDocs)

	r.Register(&types.ToolDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &types.Schema{
			Type: "OBJECT",
			Properties: map[string]*types.Schema{
				"method": {
					Type:        "STRING",
					Description: "HTTP method (GET, POST, PUT, DELETE, etc.).",
				},
				"url": {
					Type:        "STRING",
					Description: "The target URL.",
				},
				"headers": {
					Type:        "OBJECT",
					Description: "HTTP headers as a map of strings.",
					Properties: map[string]*types.Schema{
						"Content-Type": {Type: "STRING"},
					},
				},
				"body": {
					Type:        "STRING",
					Description: "Request body content.",
				},
			},
			Required: []string{"method", "url"},
		},
	}, m.httpRequest)

	r.RegisterWithOptions(&types.ToolDeclaration{
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
	}, m.registerSafePathTool, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, m.listSafePathsTool)

	r.RegisterWithOptions(&types.ToolDeclaration{
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
	}, m.removeSafePathTool, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
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
	}, m.registerReadOnlyPathTool, ToolOptions{Serial: true, LongRunning: true})

	r.Register(&types.ToolDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, m.listReadOnlyPathsTool)

	r.RegisterWithOptions(&types.ToolDeclaration{
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
	}, m.removeReadOnlyPathTool, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, m.bypassConfirmationTool, ToolOptions{Serial: true, LongRunning: true})

	r.RegisterWithOptions(&types.ToolDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, m.revokeBypassTool, ToolOptions{Serial: true, LongRunning: true})
}

func (m *systemManager) revokeBypassTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	m.sm.bypassMu.Lock()
	m.sm.bypassConfirmations = false
	m.sm.bypassMu.Unlock()

	m.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;32m[SECURITY] Interactive security prompts have been RE-ENABLED.\033[0m\n")
	m.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return types.ToolResult{Text: "Interactive security prompts have been re-enabled."}, nil
}

func (m *systemManager) bypassConfirmationTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	if m.sm.IsBypassActive() {
		return types.ToolResult{Text: "Bypass mode is already enabled."}, nil
	}

	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting to DISABLE ALL interactive security prompts.\033[0m\n")
	fmt.Fprintf(os.Stderr, "This allows the AI to execute commands and write files without further confirmation.\n")
	fmt.Fprintf(os.Stderr, "Enable bypass mode for this run? (y/N) ")

	char, err := readSingleKey(ctx)
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil {
		return types.ToolResult{}, err
	}
	if char != "y" {
		return types.ToolResult{Text: "Bypass mode denied by user."}, nil
	}

	m.sm.bypassMu.Lock()
	m.sm.bypassConfirmations = true
	m.sm.bypassMu.Unlock()

	m.sm.SaveBypassState(ctx)
	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.\033[0m\n")
	m.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return types.ToolResult{Text: "All future confirmations in this session will be bypassed. This setting is now persistent for this session name."}, nil
}

func (m *systemManager) listSafePathsTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	paths := m.sm.GetSafePaths()
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

func (m *systemManager) listReadOnlyPathsTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	paths := m.sm.GetReadOnlyPaths()
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

func (m *systemManager) removeSafePathTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

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
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		m.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := m.sm.RemoveSafePath(absPath); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := m.sm.SaveSafePaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from authorized boundaries.", absPath)}, nil
}

func (m *systemManager) removeReadOnlyPathTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

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
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of read-only authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE read-only authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Removal denied by user."}, nil
		}
		m.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := m.sm.RemoveReadOnlyPath(absPath); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error: %v", err)}, nil
	}

	if err := m.sm.SaveReadOnlyPaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath)}, nil
}

func (m *systemManager) registerSafePathTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

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
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read/write files in this location in future sessions.\033[0m (y/N) ")
		char, err = readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		m.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	m.sm.RegisterSafePath(absPath)
	if err := m.sm.SaveSafePaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path authorized but failed to persist: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized and persisted.", absPath)}, nil
}

func (m *systemManager) registerReadOnlyPathTool(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

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
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Read-only authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (auto-approved via bypass_confirmation)")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting persistent READ-ONLY access to:\033[0m %s\n", absPath)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "Authorize this path for reading? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (first confirmation)."}, nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read files in this location in future sessions.\033[0m (y/N) ")
		char, err = readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char != "y" {
			return types.ToolResult{Text: "Access denied by user (double confirmation)."}, nil
		}
		m.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	m.sm.RegisterReadOnlyPath(absPath)
	if err := m.sm.SaveReadOnlyPaths(ctx); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Path authorized for reading but failed to persist: %v", err)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Path '%s' has been successfully authorized for reading and persisted.", absPath)}, nil
}

func (m *systemManager) askUser(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	var params struct {
		Question string `json:"question"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	question := params.Question
	if question == "" {
		return types.ToolResult{}, fmt.Errorf("question argument is required")
	}

	// Tell-me style: Question in magenta, followed by "Answer > " prompt
	fmt.Fprintf(os.Stderr, "\033[1;35m[AI Question] %s\033[0m\n", question)
	fmt.Fprintf(os.Stderr, "Answer > ")

	type result struct {
		s   string
		err error
	}
	resChan := make(chan result, 1)

	go func() {
		// Use a simple scanner to avoid buffering issues with bufio.Reader if called multiple times
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			resChan <- result{scanner.Text(), nil}
		} else {
			resChan <- result{"", scanner.Err()}
		}
	}()

	select {
	case <-ctx.Done():
		return types.ToolResult{}, ctx.Err()
	case res := <-resChan:
		if res.err != nil {
			return types.ToolResult{}, fmt.Errorf("failed to read user response: %w", res.err)
		}
		return types.ToolResult{Text: strings.TrimSpace(res.s)}, nil
	}
}

func (m *systemManager) readExternalDocs(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		URL string `json:"url"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	url := params.URL
	if url == "" {
		return types.ToolResult{}, fmt.Errorf("url argument is required")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.ToolResult{}, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Limit reader to prevent DoS
	limitReader := io.LimitReader(resp.Body, 50001)
	body, err := io.ReadAll(limitReader)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to read response body: %w", err)
	}

	content := string(body)

	// Basic HTML stripping
	// 1. Remove script and style tags and their contents
	reStyle := regexp.MustCompile(`(?s)<style.*?>.*?</style>`)
	reScript := regexp.MustCompile(`(?s)<script.*?>.*?</script>`)
	content = reStyle.ReplaceAllString(content, "")
	content = reScript.ReplaceAllString(content, "")

	// 2. Remove all other HTML tags
	reTags := regexp.MustCompile(`<.*?>`)
	content = reTags.ReplaceAllString(content, " ")

	// 3. Clean up whitespace
	reSpace := regexp.MustCompile(`\n\s*\n`)
	content = reSpace.ReplaceAllString(content, "\n\n")
	content = strings.Join(strings.Fields(content), " ")

	// Truncate to avoid huge inputs
	if len(content) > 10000 {
		content = content[:10000] + "\n... (truncated)"
	}

	return types.ToolResult{Text: content}, nil
}

func (m *systemManager) httpRequest(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	var params struct {
		Method  string            `json:"method"`
		URL     string            `json:"url"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	method := params.Method
	url := params.URL
	bodyStr := params.Body

	func() {
		m.sm.TerminalLock()
		defer m.sm.TerminalUnlock()
		fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] HTTP %s %s\033[0m\n", method, url)
	}()

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to create request: %w", err)
	}

	for k, v := range params.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Limit reader to 5MB to prevent DoS
	limitReader := io.LimitReader(resp.Body, 5*1024*1024+1)
	respBody, err := io.ReadAll(limitReader)
	if err != nil {
		return types.ToolResult{}, fmt.Errorf("failed to read response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %s\n", resp.Status))
	sb.WriteString("Headers:\n")
	for k, v := range resp.Header {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", k, strings.Join(v, ", ")))
	}
	sb.WriteString("\nBody:\n")

	respBodyStr := string(respBody)
	if len(respBodyStr) > 5*1024*1024 {
		respBodyStr = respBodyStr[:5*1024*1024] + "\n... (truncated due to size limit)"
	}
	sb.WriteString(respBodyStr)

	return types.ToolResult{Text: sb.String()}, nil
}

func (m *systemManager) executeCommand(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	var params struct {
		Command    string `json:"command"`
		Reason     string `json:"reason"`
		OutputFile string `json:"output_file"`
		Append     bool   `json:"append"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	command := params.Command
	if command == "" {
		return types.ToolResult{}, fmt.Errorf("command argument is required")
	}

	reason := params.Reason
	outputFile := params.OutputFile
	appendMode := params.Append

	if outputFile != "" {
		resolvedFile, err := m.sm.IsPathWritable(outputFile)
		if err != nil {
			return types.ToolResult{}, err
		}
		outputFile = resolvedFile
	}

	approved := false

	// 1. Check for Auto-Approval (Safe read-only commands or bypass enabled)
	safe := m.isSafeCommand(command)
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Execution auto-approved (bypass_confirmation enabled).\033[0m\n")
		approved = true
	} else if safe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe command detected.\033[0m\n")
		approved = true
	} else {
		// 2. Safety Confirmation Gate (Tell-me style)
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Command: \033[0m%s\n", command)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		if outputFile != "" {
			redir := ">"
			if appendMode {
				redir = ">>"
			}
			fmt.Fprintf(os.Stderr, "\033[0;34mRedirect: %s %s\033[0m\n", redir, outputFile)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this command? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n") // New line after key hit

		if err != nil {
			return types.ToolResult{}, err
		}
		if char == "y" {
			approved = true
		}
	}

	if !approved {
		return types.ToolResult{Text: fmt.Sprintf("User denied execution of command: %s", command)}, nil
	}

	// 2.5 Log command execution if log file is set
	logSuffix := ""
	if m.sm.IsBypassActive() {
		logSuffix = " (auto-approved via bypass_confirmation)"
	}
	m.sm.logAudit("REASON", reason, "COMMAND", command+logSuffix)

	// 3. Execution
	fmt.Fprintf(os.Stderr, "\033[90mExecuting... (Output shown below)\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	parts, err := splitCommand(command)
	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Error parsing command: %v", err)}, nil
	}
	if len(parts) == 0 {
		return types.ToolResult{Text: "Error: Empty command"}, nil
	}

	// Direct binary execution (no shell)
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)

	// Stream output to stderr and capture it
	var sb strings.Builder
	const maxCapture = 1024 * 1024 // 1MB Memory Cap
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	multi := io.MultiReader(stdout, stderr)

	var file *os.File
	if outputFile != "" {
		flags := os.O_CREATE | os.O_WRONLY
		if appendMode {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		var err error
		file, err = os.OpenFile(outputFile, flags, 0644)
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("failed to open output file: %w", err)
		}
		defer file.Close()
	}

	if err := cmd.Start(); err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Command failed to start: %v", err)}, nil
	}

	scanner := bufio.NewScanner(multi)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(os.Stderr, "  \033[90m%s\033[0m\n", line)
		if sb.Len() < maxCapture {
			sb.WriteString(line + "\n")
		}
		if file != nil {
			file.WriteString(line + "\n")
		}
	}

	if err := scanner.Err(); err != nil {
		errNote := fmt.Sprintf("\n[Warning] Output read error: %v", err)
		if err == bufio.ErrTooLong {
			errNote = "\n[Warning] Output line too long for scanner; truncated."
		}
		fmt.Fprintln(os.Stderr, errNote)
		sb.WriteString(errNote + "\n")
	}

	err = cmd.Wait()
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	output := sb.String()
	if len(output) > 50000 {
		output = output[:50000] + "\n... (truncated)"
	}

	if err != nil {
		return types.ToolResult{Text: fmt.Sprintf("Exit Code: 1\nError/Output:\n%s", output)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Exit Code: 0\nOutput:\n%s", output)}, nil
}

func (m *systemManager) pipeCommands(ctx context.Context, args map[string]interface{}) (types.ToolResult, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	var params struct {
		Commands   []string `json:"commands"`
		Reason     string   `json:"reason"`
		OutputFile string   `json:"output_file"`
		Append     bool     `json:"append"`
	}
	if err := UnmarshalArgs(args, &params); err != nil {
		return types.ToolResult{}, err
	}

	commands := params.Commands
	if len(commands) < 2 {
		return types.ToolResult{}, fmt.Errorf("at least two commands are required for piping")
	}

	reason := params.Reason
	outputFile := params.OutputFile
	appendMode := params.Append

	if outputFile != "" {
		resolvedFile, err := m.sm.IsPathWritable(outputFile)
		if err != nil {
			return types.ToolResult{}, err
		}
		outputFile = resolvedFile
	}

	// Safety check
	allSafe := true
	for _, cmd := range commands {
		if !m.isSafeCommand(cmd) {
			allSafe = false
			break
		}
	}

	approved := false
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Pipeline auto-approved (bypass_confirmation enabled).\033[0m\n")
		approved = true
	} else if allSafe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe pipeline detected.\033[0m\n")
		approved = true
	} else {
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Pipeline: \033[0m%s\n", strings.Join(commands, " | "))
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		if outputFile != "" {
			redir := ">"
			if appendMode {
				redir = ">>"
			}
			fmt.Fprintf(os.Stderr, "\033[0;34mRedirect Final Output: %s %s\033[0m\n", redir, outputFile)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this pipeline? (y/N) ")

		char, err := readSingleKey(ctx)
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil {
			return types.ToolResult{}, err
		}
		if char == "y" {
			approved = true
		}
	}

	if !approved {
		return types.ToolResult{Text: "User denied execution of pipeline."}, nil
	}

	m.sm.logAudit("REASON", reason, "PIPELINE", strings.Join(commands, " | "))

	fmt.Fprintf(os.Stderr, "\033[90mExecuting Pipeline... (Output shown below)\033[0m\n")
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	cmds := make([]*exec.Cmd, len(commands))
	var combinedStderr strings.Builder
	var stderrPipes []io.Reader

	for i, cmdStr := range commands {
		parts, err := splitCommand(cmdStr)
		if err != nil {
			return types.ToolResult{Text: fmt.Sprintf("Error parsing command at index %d: %v", i, err)}, nil
		}
		if len(parts) == 0 {
			return types.ToolResult{Text: fmt.Sprintf("Error: Empty command at index %d", i)}, nil
		}
		cmds[i] = exec.CommandContext(ctx, parts[0], parts[1:]...)

		// Capture stderr from every command in the pipeline
		se, _ := cmds[i].StderrPipe()
		stderrPipes = append(stderrPipes, se)
	}

	// Track pipes to ensure they are closed on startup failure
	var pipes []io.Closer
	for _, se := range stderrPipes {
		pipes = append(pipes, se.(io.Closer))
	}
	defer func() {
		for _, p := range pipes {
			_ = p.Close()
		}
	}()

	// Setup stdout/stdin pipes
	for i := 0; i < len(cmds)-1; i++ {
		pipe, err := cmds[i].StdoutPipe()
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("failed to create pipe for command %d: %w", i, err)
		}
		pipes = append(pipes, pipe)
		cmds[i+1].Stdin = pipe
	}

	var sb strings.Builder
	// The last command's stdout is what we primarily capture for result
	lastCmd := cmds[len(cmds)-1]
	stdout, _ := lastCmd.StdoutPipe()
	pipes = append(pipes, stdout)

	var file *os.File
	if outputFile != "" {
		flags := os.O_CREATE | os.O_WRONLY
		if appendMode {
			flags |= os.O_APPEND
		} else {
			flags |= os.O_TRUNC
		}
		var err error
		file, err = os.OpenFile(outputFile, flags, 0644)
		if err != nil {
			return types.ToolResult{}, fmt.Errorf("failed to open output file: %w", err)
		}
		defer file.Close()
	}

	// Start all commands
	for i := 0; i < len(cmds); i++ {
		if err := cmds[i].Start(); err != nil {
			return types.ToolResult{Text: fmt.Sprintf("Command %d (%s) failed to start: %v", i, commands[i], err)}, nil
		}
	}

	// Read all stderr pipes in parallel
	var wg sync.WaitGroup
	var stderrMu sync.Mutex
	const maxTotalCapture = 1024 * 1024 // 1MB total buffer cap
	for i, se := range stderrPipes {
		wg.Add(1)
		go func(idx int, r io.Reader) {
			defer wg.Done()
			scanner := bufio.NewScanner(r)
			for scanner.Scan() {
				line := scanner.Text()
				stderrMu.Lock()
				fmt.Fprintf(os.Stderr, "  \033[31m[%d] %s\033[0m\n", idx, line)
				if combinedStderr.Len() < maxTotalCapture {
					combinedStderr.WriteString(line + "\n")
				}
				stderrMu.Unlock()
			}
			if err := scanner.Err(); err != nil {
				stderrMu.Lock()
				msg := fmt.Sprintf("[Error reading stderr from pipe %d: %v]", idx, err)
				fmt.Fprintln(os.Stderr, msg)
				combinedStderr.WriteString(msg + "\n")
				stderrMu.Unlock()
			}
		}(i, se)
	}

	// Stream stdout of the last command
	stdoutScanner := bufio.NewScanner(stdout)
	for stdoutScanner.Scan() {
		line := stdoutScanner.Text()
		fmt.Fprintf(os.Stderr, "  \033[90m%s\033[0m\n", line)
		if sb.Len() < maxTotalCapture {
			sb.WriteString(line + "\n")
		}
		if file != nil {
			file.WriteString(line + "\n")
		}
	}

	if err := stdoutScanner.Err(); err != nil {
		msg := fmt.Sprintf("\n[Warning] Stdout read error: %v", err)
		if err == bufio.ErrTooLong {
			msg = "\n[Warning] Stdout line too long for scanner; truncated."
		}
		fmt.Fprintln(os.Stderr, msg)
		sb.WriteString(msg + "\n")
	}

	// Wait for all stderr to be read
	wg.Wait()

	// After all commands started successfully, Wait() will eventually close the pipes.
	// We clear the pipes slice so the deferred Close() calls don't interfere with Wait().
	pipes = nil

	// Wait for all commands in reverse order
	var lastErr error
	for i := len(cmds) - 1; i >= 0; i-- {
		err := cmds[i].Wait()
		if err != nil && i == len(cmds)-1 {
			lastErr = err
		}
	}

	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	output := sb.String()
	errStr := combinedStderr.String()

	finalRes := output
	if errStr != "" {
		finalRes = fmt.Sprintf("Output:\n%s\nErrors:\n%s", output, errStr)
	}

	if len(finalRes) > 50000 {
		finalRes = finalRes[:50000] + "\n... (truncated)"
	}

	if lastErr != nil {
		return types.ToolResult{Text: fmt.Sprintf("Pipeline failed at last command. Exit Code: 1\n%s", finalRes)}, nil
	}

	return types.ToolResult{Text: fmt.Sprintf("Pipeline completed successfully. Exit Code: 0\n%s", finalRes)}, nil
}

func splitCommand(cmd string) ([]string, error) {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return nil, err
	}
	return parts, nil
}

func (m *systemManager) isSafeCommand(command string) bool {
	// Whitelist of allowed base commands (strict exact match)
	// Side-effect-free inspection tools only for auto-approval.
	safeCommands := map[string]bool{
		"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
		"head": true, "tail": true, "wc": true, "stat": true, "date": true,
		"whoami": true, "diff": true, "git": true, "go": true,
	}

	parts, err := splitCommand(command)
	if err != nil || len(parts) == 0 {
		return false
	}
	base := parts[0]

	// 1. Check against whitelist
	if !safeCommands[base] {
		return false
	}

	// 2. Specialized Check for 'git': Only allow read-only subcommands
	if base == "git" {
		sub := ""
		for i := 1; i < len(parts); i++ {
			if strings.HasPrefix(parts[i], "-") {
				// Skip flags. If it's -C or -c, skip the next part too if it's a separate arg.
				if (parts[i] == "-C" || parts[i] == "-c") && i+1 < len(parts) {
					i++
				}
				continue
			}
			sub = parts[i]
			break
		}

		if sub == "" {
			return false
		}

		readOnlyGit := map[string]bool{
			"status": true, "log": true, "diff": true, "branch": true,
			"show": true, "blame": true, "ls-files": true, "rev-parse": true,
			"tag": true, "remote": true, "describe": true,
		}
		if !readOnlyGit[sub] {
			return false
		}
	}

	// 3. Specialized check for 'go': Only allow non-destructive subcommands
	if base == "go" {
		sub := ""
		for i := 1; i < len(parts); i++ {
			if strings.HasPrefix(parts[i], "-") {
				continue
			}
			sub = parts[i]
			break
		}
		allowedGo := map[string]bool{
			"list": true, "help": true, "version": true, "env": true,
			"vet": true,
		}
		if !allowedGo[sub] {
			return false
		}
	}

	// 4. Check for unsafe characters (pipes, redirects, expansion, etc.)
	// We are extremely strict here to prevent shell injection.
	unsafeChars := []string{"|", "&", ";", ">", "<", "$", "`", "\n", "\r"}
	for _, char := range unsafeChars {
		if strings.Contains(command, char) {
			return false
		}
	}

	// 5. Path Safety Check: Ensure all arguments stay within allowed boundaries.
	for i := 1; i < len(parts); i++ {
		arg := parts[i]
		if arg == "" || strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") {
			// Skip empty args and simple flags like -la
			continue
		}
		// Special case for Go's recursive package pattern
		if arg == "./..." || arg == "..." {
			continue
		}
		// If it's a flag with a path like --config=path
		if strings.Contains(arg, "=") && strings.HasPrefix(arg, "-") {
			arg = strings.SplitN(arg, "=", 2)[1]
		}

		if _, err := m.sm.IsPathSafe(arg); err != nil {
			// Some args might not be paths, but we try to check them anyway if they look like paths
			if strings.Contains(arg, "/") || strings.Contains(arg, "\\") || arg == "." || arg == ".." {
				fmt.Fprintf(os.Stderr, "\033[0;31m[Safety] %v\033[0m\n", err)
				return false
			}
		}
	}

	return true
}
