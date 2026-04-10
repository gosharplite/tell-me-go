// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/google/shlex"
	domain "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/stringsutil"
)

// commandValidator handles command validation and security checks.
type commandValidator struct {
	sm         domain.Manager
	safety     *domain.SafetyService
	interactor domain.UserInteractor
	goos       string // The operating system for platform-specific logic
}

// NewCommandValidator creates a new commandValidator.
func NewCommandValidator(sm domain.Manager, interactor domain.UserInteractor) domain.CommandValidator {
	var safety *domain.SafetyService
	if sm != nil {
		if ism, ok := sm.(internalSecurityProvider); ok {
			safety = ism.getSafetyService()
		}
	}
	if safety == nil {
		safety = domain.NewSafetyService(domain.DefaultPolicy())
	}
	return &commandValidator{sm: sm, safety: safety, interactor: interactor, goos: runtime.GOOS}
}

// IsSafe checks if a command is safe for auto-approval.
// Returns (isSafe, reason if unsafe).
func (v *commandValidator) IsSafe(command string) (bool, string) {
	parts, err := v.Split(command)
	if err != nil {
		return false, fmt.Sprintf("failed to parse command: %v", err)
	}
	if len(parts) == 0 {
		return false, "empty command"
	}

	// 1 & 2. Whitelist checks
	if safe, reason := v.validateWhitelists(parts[0]); !safe {
		return false, reason
	}

	// 3 & 4. Specialized subcommand checks
	if safe, reason := v.validateSubcommandSpecifics(parts); !safe {
		return false, reason
	}

	// 5. Check for unsafe characters (pipes, redirects, expansion, etc.)
	if safe, reason := v.hasUnsafeChars(command); !safe {
		return false, reason
	}

	// 6. Path Safety Check: Ensure all arguments stay within allowed boundaries.
	return v.CheckPathSafety(parts)
}

func (v *commandValidator) validateWhitelists(base string) (bool, string) {
	return v.safety.IsCommandSafe(base)
}

func (v *commandValidator) validateSubcommandSpecifics(parts []string) (bool, string) {
	base := parts[0]
	switch base {
	case "git":
		return v.isSafeGit(parts)
	case "go":
		return v.isSafeGo(parts)
	case "gh":
		return v.isSafeGh(parts)
	case "az":
		return v.isSafeAz(parts)
	}
	return true, ""
}

// Split uses shlex to split a command string into arguments.
func (v *commandValidator) Split(cmd string) ([]string, error) {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return nil, fmt.Errorf("shlex split error: %w", err)
	}

	// Path Integrity Check (Issue #44)
	// Detect if backslashes in Windows paths were stripped by the POSIX-style parser.
	if strings.Contains(cmd, "\\") {
		partsCount := 0
		for _, part := range parts {
			partsCount += strings.Count(part, "\\")
		}

		if partsCount < strings.Count(cmd, "\\") {
			if v.isLikelyWindowsPath(cmd) {
				return nil, fmt.Errorf("possible path corruption detected: backslashes in Windows paths are stripped by the POSIX-style parser. Please use forward slashes (/) instead (e.g., 'C:/Users' or './path/to/file')")
			}
		}
	}

	return parts, nil
}

func (v *commandValidator) isLikelyWindowsPath(cmd string) bool {
	if strings.Contains(cmd, ":\\") || strings.Contains(cmd, "\\\\") {
		return true
	}
	for i := 0; i < len(cmd)-1; i++ {
		if cmd[i] == '\\' {
			next := cmd[i+1]
			// Backslash followed by alphanumeric is a strong indicator of a Windows path component
			if (next >= 'a' && next <= 'z') || (next >= 'A' && next <= 'Z') || (next >= '0' && next <= '9') {
				return true
			}
		}
	}
	return false
}

// ValidateStructure ensures the command does not contain standalone shell operators
// that would be misinterpreted during direct binary execution.
func (v *commandValidator) ValidateStructure(parts []string) error {
	if len(parts) == 0 {
		return nil
	}

	// 1. Skip structural syntax limits if the intent is to use a shell.
	// The shell safely evaluates interpolation and the entire string is still
	// subject to the IsSafe prompt/authorization flow anyway.
	isShell := false
	switch parts[0] {
	case "sh", "bash", "zsh", "ksh", "csh", "cmd.exe", "cmd", "powershell", "pwsh":
		isShell = true
	}

	if !isShell {
		if ok, desc := v.safety.HasForbiddenOperators(parts); ok {
			return fmt.Errorf("standalone shell operator (%s) detected. "+
				"This tool executes binaries directly and does not support shell features. "+
				"To use shell features, wrap the command: sh -c \"your command\"", desc)
		}

		for i, part := range parts {
			if err := v.validateTokenStructure(part, i == 0); err != nil {
				return err
			}
		}
	}
	return nil
}

func (v *commandValidator) validateTokenStructure(token string, isFirstToken bool) error {
	// Check for interpolation characters in any token to prevent shell-like behavior
	// in binaries that might evaluate their arguments.
	if v.safety.HasUnsafeInterpolation(token) {
		return fmt.Errorf("shell interpolation character detected in token '%s'. "+
			"This tool executes binaries directly and does not support shell expansion. "+
			"To use shell features, wrap the command: sh -c \"your command\"", token)
	}

	// Check for attached operators like "ls;echo" or "ls>out"
	// We only apply this to the first token (the command) to minimize false positives
	// in arguments (e.g., grep "a;b") while still catching common mistakes.
	if isFirstToken && v.safety.HasForbiddenCharsInCommand(token) {
		return fmt.Errorf("shell operator detected inside command token '%s'. "+
			"This tool executes binaries directly and does not support shell features. "+
			"To use shell features, wrap the command: sh -c \"your command\"", token)
	}

	return nil
}

// truncateOutput limits a string to a maximum number of lines, appending a truncation message if needed.
func truncateOutput(output string, maxLines int) string {
	return stringsutil.TruncateOutput(output, maxLines)
}

func extractSubcommand(parts []string) string {
	for i := 1; i < len(parts); i++ {
		if strings.HasPrefix(parts[i], "-") {
			// Skip flags. If it's -C or -c, skip the next part too if it's a separate arg.
			if (parts[i] == "-C" || parts[i] == "-c") && i+1 < len(parts) {
				i++
			}
			continue
		}
		return parts[i]
	}
	return ""
}

func (v *commandValidator) isSafeGit(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	if sub == "" {
		return false, "missing git subcommand"
	}

	if !v.safety.IsSafeGitSubcommand(sub) {
		return false, fmt.Sprintf("git subcommand '%s' is not in the safe whitelist", sub)
	}
	return true, ""
}

func (v *commandValidator) isSafeGo(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	if !v.safety.IsSafeGoSubcommand(sub) {
		return false, fmt.Sprintf("go subcommand '%s' is not in the safe whitelist", sub)
	}

	var err error
	switch sub {
	case "test":
		err = v.validateGoTest(parts)
	case "tool":
		err = v.validateGoTool(parts)
	}

	if err != nil {
		return false, err.Error()
	}

	return true, ""
}

func (v *commandValidator) isSafeGh(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	if sub == "" {
		return false, "missing gh subcommand"
	}

	if !v.safety.IsSafeGhSubcommand(sub) {
		return false, fmt.Sprintf("gh subcommand '%s' is not in the safe whitelist", sub)
	}
	return true, ""
}

func (v *commandValidator) isSafeAz(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	if sub == "" {
		return false, "missing az subcommand"
	}

	if !v.safety.IsSafeAzSubcommand(sub) {
		return false, fmt.Sprintf("az subcommand '%s' is not in the safe whitelist", sub)
	}
	return true, ""
}

func (v *commandValidator) validateGoTest(parts []string) error {
	for _, arg := range parts {
		if strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "--output") {
			return fmt.Errorf("go test with output redirection is not auto-approvable")
		}
	}
	return nil
}

func (v *commandValidator) validateGoTool(parts []string) error {
	isCover := false
	for _, arg := range parts {
		if arg == "cover" {
			isCover = true
			break
		}
	}
	if !isCover {
		return fmt.Errorf("only 'go tool cover' is authorized for auto-approval")
	}
	return nil
}

func (v *commandValidator) hasUnsafeChars(command string) (bool, string) {
	// We are extremely strict here to prevent shell injection.
	unsafeChars := []struct {
		char   string
		reason string
	}{
		{"|", "pipes are not allowed in single commands"},
		{"&", "background execution or logical AND is not allowed"},
		{";", "command chaining is not allowed"},
		{">", "redirection is not allowed (use output_file parameter)"},
		{"<", "input redirection is not allowed"},
		{"$", "variable expansion is not allowed"},
		{"`", "command substitution is not allowed"},
		{"*", "wildcards are not allowed in auto-approvable commands"},
		{"?", "wildcards are not allowed in auto-approvable commands"},
		{"[", "wildcards are not allowed in auto-approvable commands"},
		{"\n", "newlines are not allowed"},
		{"\r", "carriage returns are not allowed"},
	}
	for _, uc := range unsafeChars {
		if strings.Contains(command, uc.char) {
			// EXCEPTION: Allow $ in 'go test' for regex anchors like -run=^$
			if uc.char == "$" && strings.HasPrefix(command, "go test") {
				continue
			}
			return false, uc.reason
		}
	}
	return true, ""
}

// CheckPathSafety ensures all arguments stay within allowed boundaries.
func (v *commandValidator) CheckPathSafety(parts []string) (bool, string) {
	for i := 1; i < len(parts); i++ {
		cleaned := cleanPathArgument(parts[i])
		if cleaned != "" {
			if safe, reason := v.validateSinglePath(cleaned); !safe {
				return false, reason
			}
		}
	}
	return true, ""
}

// HasShellFeatures checks if the command parts contain any shell operators,
// wildcards, or interpolation characters that require a shell interpreter.
func (v *commandValidator) HasShellFeatures(parts []string) bool {
	if len(parts) == 0 {
		return false
	}

	// 1. Skip if already calling a shell explicitly as the primary command
	switch parts[0] {
	case "sh", "bash", "zsh", "ksh", "csh", "cmd.exe", "cmd", "powershell", "pwsh":
		return false
	}

	// 2. Check for standalone shell operators (e.g. &&, ||, ;, |, >, <)
	if ok, _ := v.safety.HasForbiddenOperators(parts); ok {
		return true
	}

	for i, part := range parts {
		// 3. Check for shell interpolation ($ or `) and wildcards (*, ?, [])
		if v.safety.HasUnsafeInterpolation(part) || strings.ContainsAny(part, "*?[]") {
			return true
		}

		// 4. Check for attached operators in the command token (e.g. "ls;echo")
		if i == 0 && v.safety.HasForbiddenCharsInCommand(part) {
			return true
		}

		// 5. Check for PowerShell Verb-Noun pattern in the command token (e.g. "Get-ChildItem")
		if i == 0 && v.isPowerShellCmdlet(part) {
			return true
		}

		// 6. Check for Windows shell built-in commands (e.g. "del", "dir")
		if i == 0 && v.goos == "windows" && v.isWindowsBuiltIn(part) {
			return true
		}
	}

	return false
}

func (v *commandValidator) isWindowsBuiltIn(token string) bool {
	builtIns := map[string]bool{
		"del": true, "erase": true, "dir": true, "echo": true, "mkdir": true,
		"md": true, "rmdir": true, "rd": true, "copy": true, "move": true,
		"ren": true, "rename": true, "type": true, "cls": true, "ver": true,
		"vol": true, "set": true, "path": true, "pause": true, "exit": true,
		"prompt": true, "title": true, "color": true, "start": true,
	}
	return builtIns[strings.ToLower(token)]
}

func (v *commandValidator) isPowerShellCmdlet(token string) bool {
	dashIdx := strings.Index(token, "-")
	if dashIdx <= 0 || dashIdx == len(token)-1 {
		return false
	}
	verb := token[:dashIdx]
	// Verbs are typically 2+ characters
	if len(verb) < 2 {
		return false
	}
	// Common PowerShell verbs: Get, Set, New, Remove, Update, Invoke, etc.
	// We'll use a simple heuristic: if it's Verb-Noun and not a known binary like "git-lfs"
	// (though git-lfs would usually be called as "git lfs").
	// To be safe, we check if the verb starts with an uppercase letter or is a common verb.
	firstChar := verb[0]
	if firstChar >= 'A' && firstChar <= 'Z' {
		return true
	}
	// Also allow lowercase common verbs for convenience
	commonVerbs := []string{"get", "set", "new", "remove", "update", "invoke", "test", "write", "read", "copy", "move", "clear", "add"}
	for _, v := range commonVerbs {
		if strings.EqualFold(verb, v) {
			return true
		}
	}
	return false
}

func cleanPathArgument(arg string) string {
	if arg == "" {
		return ""
	}
	// Skip simple flags like -la
	if strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=") {
		return ""
	}
	// Handle the key-value case: if the argument contains `=`, split it and return only the value part.
	if strings.Contains(arg, "=") {
		parts := strings.SplitN(arg, "=", 2)
		return parts[1]
	}
	return arg
}

func (v *commandValidator) validateSinglePath(arg string) (bool, string) {
	if v.isSpecialPattern(arg) {
		return true, ""
	}

	if _, err := v.sm.IsPathSafe(arg); err != nil {
		if v.looksLikePath(arg) {
			if v.sm != nil {
				v.sm.Warn(fmt.Sprintf("[Safety] %v", err))
			} else if v.interactor != nil {
				v.interactor.Warn(fmt.Sprintf("[Safety] %v", err))
			}
			return false, fmt.Sprintf("path safety check failed for argument '%s': %v", arg, err)
		}
	}
	return true, ""
}

func (v *commandValidator) isSpecialPattern(arg string) bool {
	return arg == "" || arg == "./..." || arg == "..."
}

func (v *commandValidator) looksLikePath(arg string) bool {
	return strings.Contains(arg, "/") || strings.Contains(arg, "\\") || arg == "." || arg == ".."
}
