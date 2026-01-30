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
	"time"

	"google.golang.org/genai"
)

type systemManager struct {
	sm *SecurityManager
}

// RegisterSystemTools adds system-related tools to the registry.
func RegisterSystemTools(r *Registry, sm *SecurityManager) {
	m := &systemManager{sm: sm}

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "execute_command",
		Description: "Executes a shell command on the local system.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"command": {
					Type:        genai.TypeString,
					Description: "The shell command to execute (e.g., 'ls -la', 'go test ./...').",
				},
				"reason": {
					Type:        genai.TypeString,
					Description: "A short explanation of why this command needs to be executed.",
				},
			},
			Required: []string{"command"},
		},
	}, m.executeCommand, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "ask_user",
		Description: "Asks the user a specific question to clarify requirements or request confirmation.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"question": {
					Type:        genai.TypeString,
					Description: "The question to ask the user.",
				},
			},
			Required: []string{"question"},
		},
	}, m.askUser, ToolOptions{Serial: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_url",
		Description: "Fetches the content of a specific URL.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "The URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, m.readURL)

	r.Register(&genai.FunctionDeclaration{
		Name:        "read_external_docs",
		Description: "Fetches the content of a specific URL and cleans it into readable documentation.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"url": {
					Type:        genai.TypeString,
					Description: "The documentation URL to fetch.",
				},
			},
			Required: []string{"url"},
		},
	}, m.readExternalDocs)

	r.Register(&genai.FunctionDeclaration{
		Name:        "http_request",
		Description: "Executes a custom HTTP request.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"method": {
					Type:        genai.TypeString,
					Description: "HTTP method (GET, POST, PUT, DELETE, etc.).",
				},
				"url": {
					Type:        genai.TypeString,
					Description: "The target URL.",
				},
				"headers": {
					Type:        genai.TypeObject,
					Description: "HTTP headers as a map of strings.",
				},
				"body": {
					Type:        genai.TypeString,
					Description: "Request body content.",
				},
			},
			Required: []string{"method", "url"},
		},
	}, m.httpRequest)

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "register_safepath",
		Description: "Adds a directory or file to the allowed boundaries for AI access. This is a persistent configuration.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The absolute or relative path to authorize.",
				},
				"reason": {
					Type:        genai.TypeString,
					Description: "Reason why this path needs to be authorized.",
				},
			},
			Required: []string{"path", "reason"},
		},
	}, m.registerSafePathTool, ToolOptions{Serial: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "list_safepaths",
		Description: "Lists all currently authorized safe paths and files.",
	}, m.listSafePathsTool)

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "remove_safepath",
		Description: "Removes a directory or file from the authorized boundaries.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The path to remove from authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, m.removeSafePathTool, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "register_readpath",
		Description: "Adds a directory or file to the allowed boundaries for READ-ONLY access. This is a persistent configuration.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The absolute or relative path to authorize for reading.",
				},
				"reason": {
					Type:        genai.TypeString,
					Description: "Reason why this path needs to be authorized.",
				},
			},
			Required: []string{"path", "reason"},
		},
	}, m.registerReadOnlyPathTool, ToolOptions{Serial: true})

	r.Register(&genai.FunctionDeclaration{
		Name:        "list_readpaths",
		Description: "Lists all currently authorized read-only paths and files.",
	}, m.listReadOnlyPathsTool)

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "remove_readpath",
		Description: "Removes a directory or file from the read-only authorized boundaries.",
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"path": {
					Type:        genai.TypeString,
					Description: "The path to remove from read-only authorized boundaries.",
				},
			},
			Required: []string{"path"},
		},
	}, m.removeReadOnlyPathTool, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "bypass_confirmation",
		Description: "Disables all interactive security prompts for the current session. This setting is persistent for the session until revoked or a new session is started.",
	}, m.bypassConfirmationTool, ToolOptions{Serial: true})

	r.RegisterWithOptions(&genai.FunctionDeclaration{
		Name:        "revoke_bypass",
		Description: "Re-enables interactive security prompts by revoking the bypass status.",
	}, m.revokeBypassTool, ToolOptions{Serial: true})
}

func (m *systemManager) revokeBypassTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	m.sm.bypassMu.Lock()
	m.sm.bypassConfirmations = false
	m.sm.bypassMu.Unlock()

	m.sm.SaveBypassState()
	fmt.Fprintf(os.Stderr, "\033[1;32m[SECURITY] Interactive security prompts have been RE-ENABLED.\033[0m\n")
	m.sm.logAudit("ACTION", "REVOKE BYPASS", "DETAIL", "Bypass status revoked by AI/User.")
	return "Interactive security prompts have been re-enabled.", nil
}

func (m *systemManager) bypassConfirmationTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	if m.sm.IsBypassActive() {
		return "Bypass mode is already enabled.", nil
	}

	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] AI is requesting to DISABLE ALL interactive security prompts.\033[0m\n")
	fmt.Fprintf(os.Stderr, "This allows the AI to execute commands and write files without further confirmation.\n")
	fmt.Fprintf(os.Stderr, "Enable bypass mode for this run? (y/N) ")

	char, err := readSingleKey()
	fmt.Fprintf(os.Stderr, "\n")
	if err != nil || char != "y" {
		return "Bypass mode denied by user.", nil
	}

	m.sm.bypassMu.Lock()
	m.sm.bypassConfirmations = true
	m.sm.bypassMu.Unlock()

	m.sm.SaveBypassState()
	fmt.Fprintf(os.Stderr, "\033[1;31m[SECURITY] ALL INTERACTIVE CONFIRMATIONS HAVE BEEN DISABLED FOR THIS SESSION.\033[0m\n")
	m.sm.logAudit("ACTION", "BYPASS CONFIRMATION", "DETAIL", "User manually approved bypass of all interactive security prompts for this session.")
	return "All future confirmations in this session will be bypassed. This setting is now persistent for this session name.", nil
}

func (m *systemManager) listSafePathsTool(ctx context.Context, args map[string]interface{}) (string, error) {
	paths := m.sm.GetSafePaths()
	if len(paths) == 0 {
		return "No additional safe paths are currently registered.", nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized safe paths:\n")
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("- %s\n", p))
	}
	return sb.String(), nil
}

func (m *systemManager) listReadOnlyPathsTool(ctx context.Context, args map[string]interface{}) (string, error) {
	paths := m.sm.GetReadOnlyPaths()
	if len(paths) == 0 {
		return "No additional read-only paths are currently registered.", nil
	}

	var sb strings.Builder
	sb.WriteString("Currently authorized read-only paths:\n")
	for _, p := range paths {
		sb.WriteString(fmt.Sprintf("- %s\n", p))
	}
	return sb.String(), nil
}

func (m *systemManager) removeSafePathTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}

	// Confirmation Gate
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Removal denied by user.", nil
		}
		m.sm.logAudit("ACTION", "REMOVE SAFEPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := m.sm.RemoveSafePath(absPath); err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	if err := m.sm.SaveSafePaths(); err != nil {
		return fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err), nil
	}

	return fmt.Sprintf("Path '%s' has been successfully removed from authorized boundaries.", absPath), nil
}

func (m *systemManager) removeReadOnlyPathTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	path, _ := args["path"].(string)
	if path == "" {
		return "", fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
	}

	// Confirmation Gate
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Removal of read-only authorization auto-approved.\033[0m\n")
		m.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "auto-approved via bypass_confirmation")
	} else {
		fmt.Fprintf(os.Stderr, "\033[1;33m[SECURITY] AI is requesting to REMOVE read-only authorization for:\033[0m %s\n", absPath)
		fmt.Fprintf(os.Stderr, "Confirm removal? (y/N) ")

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Removal denied by user.", nil
		}
		m.sm.logAudit("ACTION", "REMOVE READPATH on "+absPath, "DETAIL", "User manually approved")
	}

	if err := m.sm.RemoveReadOnlyPath(absPath); err != nil {
		return fmt.Sprintf("Error: %v", err), nil
	}

	if err := m.sm.SaveReadOnlyPaths(); err != nil {
		return fmt.Sprintf("Path removed from memory but failed to update persistence: %v", err), nil
	}

	return fmt.Sprintf("Path '%s' has been successfully removed from read-only authorized boundaries.", absPath), nil
}

func (m *systemManager) registerSafePathTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	path, _ := args["path"].(string)
	reason, _ := args["reason"].(string)

	if path == "" {
		return "", fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
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

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Access denied by user (first confirmation).", nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read/write files in this location in future sessions.\033[0m (y/N) ")
		char, err = readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Access denied by user (double confirmation).", nil
		}
		m.sm.logAudit("ACTION", "REGISTER SAFEPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	m.sm.RegisterSafePath(absPath)
	if err := m.sm.SaveSafePaths(); err != nil {
		return fmt.Sprintf("Path authorized but failed to persist: %v", err), nil
	}

	return fmt.Sprintf("Path '%s' has been successfully authorized and persisted.", absPath), nil
}

func (m *systemManager) registerReadOnlyPathTool(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	path, _ := args["path"].(string)
	reason, _ := args["reason"].(string)

	if path == "" {
		return "", fmt.Errorf("path argument is required")
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("invalid path: %v", err)
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

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Access denied by user (first confirmation).", nil
		}

		// 2. Double Confirmation
		fmt.Fprintf(os.Stderr, "\033[1;31m[DOUBLE CONFIRM] Are you absolutely sure? This allows the AI to read files in this location in future sessions.\033[0m (y/N) ")
		char, err = readSingleKey()
		fmt.Fprintf(os.Stderr, "\n")
		if err != nil || char != "y" {
			return "Access denied by user (double confirmation).", nil
		}
		m.sm.logAudit("ACTION", "REGISTER READPATH on "+absPath, "DETAIL", "Reason: "+reason+" (User manually double-confirmed)")
	}

	// Register and Persist
	m.sm.RegisterReadOnlyPath(absPath)
	if err := m.sm.SaveReadOnlyPaths(); err != nil {
		return fmt.Sprintf("Path authorized for reading but failed to persist: %v", err), nil
	}

	return fmt.Sprintf("Path '%s' has been successfully authorized for reading and persisted.", absPath), nil
}

func (m *systemManager) askUser(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	question, ok := args["question"].(string)
	if !ok || question == "" {
		return "", fmt.Errorf("question argument is required")
	}

	// Tell-me style: Question in magenta, followed by "Answer > " prompt
	fmt.Fprintf(os.Stderr, "\033[1;35m[AI Question] %s\033[0m\n", question)
	fmt.Fprintf(os.Stderr, "Answer > ")

	reader := bufio.NewReader(os.Stdin)
	response, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read user response: %w", err)
	}

	return strings.TrimSpace(response), nil
}

func (m *systemManager) readURL(ctx context.Context, args map[string]interface{}) (string, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("url argument is required")
	}

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	out := string(body)
	if len(out) > 50000 {
		out = out[:50000] + "\n... (truncated)"
	}

	return out, nil
}

func (m *systemManager) readExternalDocs(ctx context.Context, args map[string]interface{}) (string, error) {
	content, err := m.readURL(ctx, args)
	if err != nil {
		return "", err
	}

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

	return content, nil
}

func (m *systemManager) httpRequest(ctx context.Context, args map[string]interface{}) (string, error) {
	method, _ := args["method"].(string)
	url, _ := args["url"].(string)
	bodyStr, _ := args["body"].(string)

	m.sm.TerminalLock()
	fmt.Fprintf(os.Stderr, "\033[0;36m[Tool Action] HTTP %s %s\033[0m\n", method, url)
	m.sm.TerminalUnlock()

	var reqBody io.Reader
	if bodyStr != "" {
		reqBody = strings.NewReader(bodyStr)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if headers, ok := args["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if val, ok := v.(string); ok {
				req.Header.Set(k, val)
			}
		}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Status: %s\n", resp.Status))
	sb.WriteString("Headers:\n")
	for k, v := range resp.Header {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", k, strings.Join(v, ", ")))
	}
	sb.WriteString("\nBody:\n")
	sb.WriteString(string(respBody))

	out := sb.String()
	if len(out) > 10000 {
		out = out[:10000] + "\n... (truncated)"
	}

	return out, nil
}

func (m *systemManager) isSafeCommand(command string) bool {
	// Whitelist of allowed base commands (strict exact match)
	safeCommands := map[string]bool{
		"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
		"head": true, "tail": true, "wc": true, "stat": true, "date": true,
		"whoami": true, "diff": true, "awk": true, "sed": true, "git": true,
	}

	parts := strings.Fields(command)
	if len(parts) == 0 {
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
				// Note: git -Cpath is also valid, but parts[i] would be "-Cpath" and start with "-".
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

	// 3. Check for unsafe characters (pipes, redirects, expansion, etc.)
	// We are extremely strict here to prevent shell injection.
	unsafeChars := []string{"|", "&", ";", ">", "<", "$", "`", "\n", "\r"}
	for _, char := range unsafeChars {
		if strings.Contains(command, char) {
			return false
		}
	}

	// 3. Path Safety Check: Ensure all arguments stay within allowed boundaries.
	for i := 1; i < len(parts); i++ {
		arg := strings.Trim(parts[i], "\"'")
		if arg == "" || strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") {
			// Skip empty args and simple flags like -la
			continue
		}
		if err := m.sm.IsPathSafe(arg); err != nil {
			fmt.Fprintf(os.Stderr, "\033[0;31m[Safety] %v\033[0m\n", err)
			return false
		}
	}

	return true
}

func (m *systemManager) executeCommand(ctx context.Context, args map[string]interface{}) (string, error) {
	m.sm.TerminalLock()
	defer m.sm.TerminalUnlock()

	command, ok := args["command"].(string)
	if !ok || command == "" {
		return "", fmt.Errorf("command argument is required")
	}

	reason, _ := args["reason"].(string)

	approved := false

	// 1. Check for Auto-Approval (Safe read-only commands or bypass enabled)
	safe := m.isSafeCommand(command)
	if m.sm.IsBypassActive() {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Bypassed] Execution auto-approved (bypass_confirmation enabled).\033[0m\n")
		approved = true
	} else if safe {
		fmt.Fprintf(os.Stderr, "\033[0;32m[Auto-Approved] Safe read-only command detected.\033[0m\n")
		approved = true
	} else {
		// 2. Safety Confirmation Gate (Tell-me style)
		fmt.Fprintf(os.Stderr, "\033[0;36mExecute Command: \033[0m%s\n", command)
		if reason != "" {
			fmt.Fprintf(os.Stderr, "\033[0;33mReason: %s\033[0m\n", reason)
		}
		fmt.Fprintf(os.Stderr, "⚠️  Execute this command? (y/N) ")

		char, err := readSingleKey()
		fmt.Fprintf(os.Stderr, "\n") // New line after key hit

		if err == nil && (char == "y") {
			approved = true
		}
	}

	if !approved {
		return fmt.Sprintf("User denied execution of command: %s", command), nil
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

	// We use "sh -c" to allow for complex commands
	cmd := exec.CommandContext(ctx, "sh", "-c", command)

	// Stream output to stderr and capture it
	var sb strings.Builder
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	multi := io.MultiReader(stdout, stderr)

	if err := cmd.Start(); err != nil {
		return fmt.Sprintf("Command failed to start: %v", err), nil
	}

	scanner := bufio.NewScanner(multi)
	for scanner.Scan() {
		line := scanner.Text()
		fmt.Fprintf(os.Stderr, "  \033[90m%s\033[0m\n", line)
		sb.WriteString(line + "\n")
	}

	err := cmd.Wait()
	fmt.Fprintf(os.Stderr, "\033[90m------------------------------------------------------------\033[0m\n")

	output := sb.String()
	if len(output) > 50000 {
		output = output[:50000] + "\n... (truncated)"
	}

	if err != nil {
		return fmt.Sprintf("Exit Code: 1\nError/Output:\n%s", output), nil
	}

	return fmt.Sprintf("Exit Code: 0\nOutput:\n%s", output), nil
}
