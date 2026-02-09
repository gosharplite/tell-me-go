// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/shlex"
	"github.com/gosharplite/tell-me-go/internal/ui"
)

// CommandValidator handles command validation and security checks.
type CommandValidator struct {
	sm SecurityProvider
}

// NewCommandValidator creates a new CommandValidator.
func NewCommandValidator(sm SecurityProvider) *CommandValidator {
	return &CommandValidator{sm: sm}
}

var autoApprovableCommands = map[string]bool{
	"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
	"head": true, "tail": true, "wc": true, "stat": true, "date": true,
	"whoami": true, "diff": true, "git": true, "go": true,
	"golangci-lint": true, "staticcheck": true, "govulncheck": true,
}

// IsSafe checks if a command is safe for auto-approval.
// Returns (isSafe, reason if unsafe).
func (v *CommandValidator) IsSafe(command string) (bool, string) {
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

func (v *CommandValidator) validateWhitelists(base string) (bool, string) {
	// 1. Check against central security policy whitelist (Single Source of Truth)
	if !v.sm.IsCommandAllowed(base) {
		return false, fmt.Sprintf("command '%s' is not allowed by security policy", base)
	}

	// 2. Check if the command is side-effect-free (inspection only) for auto-approval.
	if !autoApprovableCommands[base] {
		return false, fmt.Sprintf("command '%s' is not in the auto-approval whitelist", base)
	}
	return true, ""
}

func (v *CommandValidator) validateSubcommandSpecifics(parts []string) (bool, string) {
	base := parts[0]
	switch base {
	case "git":
		return v.isSafeGit(parts)
	case "go":
		return v.isSafeGo(parts)
	}
	return true, ""
}

// Split uses shlex to split a command string into arguments.
func (v *CommandValidator) Split(cmd string) ([]string, error) {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return nil, fmt.Errorf("shlex split error: %w", err)
	}
	return parts, nil
}

// ValidateStructure ensures the command does not contain standalone shell operators
// that would be misinterpreted during direct binary execution.
func (v *CommandValidator) ValidateStructure(parts []string) error {
	forbidden := map[string]string{
		"&&":  "logical AND",
		"||":  "logical OR",
		";":   "command separator",
		"|":   "pipe",
		">":   "output redirection",
		">>":  "append redirection",
		"<":   "input redirection",
		"&":   "background execution",
		"2>":  "error redirection",
		"&>":  "combined redirection",
		"|&":  "combined pipe",
		"1>":  "output redirection",
		"1>>": "append redirection",
		"2>>": "error append redirection",
	}

	for i, part := range parts {
		if desc, found := forbidden[part]; found {
			return fmt.Errorf("standalone shell operator '%s' (%s) detected. "+
				"This tool executes binaries directly and does not support shell features. "+
				"To use shell features, wrap the command: sh -c \"your command\"", part, desc)
		}

		// Check for interpolation characters in any token to prevent shell-like behavior
		// in binaries that might evaluate their arguments.
		if strings.ContainsAny(part, "$`") {
			return fmt.Errorf("shell interpolation character detected in token '%s'. "+
				"This tool executes binaries directly and does not support shell expansion. "+
				"To use shell features, wrap the command: sh -c \"your command\"", part)
		}

		// Check for attached operators like "ls;echo" or "ls>out"
		// We only apply this to the first token (the command) to minimize false positives
		// in arguments (e.g., grep "a;b") while still catching common mistakes.
		if i == 0 && strings.ContainsAny(part, ";&|><\n\r") {
			return fmt.Errorf("shell operator detected inside command token '%s'. "+
				"This tool executes binaries directly and does not support shell features. "+
				"To use shell features, wrap the command: sh -c \"your command\"", part)
		}
	}
	return nil
}

// TruncateOutput limits a string to a maximum number of lines, appending a truncation message if needed.
// It is designed to be memory-efficient by avoiding a full split of the string.
func TruncateOutput(output string, maxLines int) string {
	if output == "" {
		return ""
	}
	if maxLines <= 0 {
		return "\n... (Output truncated) ..."
	}

	count := 0
	for i := 0; i < len(output); i++ {
		if output[i] == '\n' {
			count++
			if count >= maxLines {
				return output[:i] + "\n... (Output truncated) ..."
			}
		}
	}
	return output
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

func (v *CommandValidator) isSafeGit(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	if sub == "" {
		return false, "missing git subcommand"
	}

	readOnlyGit := map[string]bool{
		"status": true, "log": true, "diff": true, "branch": true,
		"show": true, "blame": true, "ls-files": true, "rev-parse": true,
		"tag": true, "remote": true, "describe": true,
	}
	if !readOnlyGit[sub] {
		return false, fmt.Sprintf("git subcommand '%s' is not in the safe whitelist", sub)
	}
	return true, ""
}

func (v *CommandValidator) isSafeGo(parts []string) (bool, string) {
	sub := extractSubcommand(parts)
	allowedGo := map[string]bool{
		"list": true, "help": true, "version": true, "env": true,
		"vet": true, "test": true, "tool": true,
	}
	if !allowedGo[sub] {
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

func (v *CommandValidator) validateGoTest(parts []string) error {
	for _, arg := range parts {
		if strings.HasPrefix(arg, "-o") || strings.HasPrefix(arg, "--output") {
			return fmt.Errorf("go test with output redirection is not auto-approvable")
		}
	}
	return nil
}

func (v *CommandValidator) validateGoTool(parts []string) error {
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

func (v *CommandValidator) hasUnsafeChars(command string) (bool, string) {
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
		{"\n", "newlines are not allowed"},
		{"\r", "carriage returns are not allowed"},
	}
	for _, uc := range unsafeChars {
		if strings.Contains(command, uc.char) {
			return false, uc.reason
		}
	}
	return true, ""
}

// CheckPathSafety ensures all arguments stay within allowed boundaries.
func (v *CommandValidator) CheckPathSafety(parts []string) (bool, string) {
	for i := 1; i < len(parts); i++ {
		arg := parts[i]
		if arg == "" || (strings.HasPrefix(arg, "-") && !strings.Contains(arg, "=")) {
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

		if _, err := v.sm.IsPathSafe(arg); err != nil {
			// Some args might not be paths, but we try to check them anyway if they look like paths
			if strings.Contains(arg, "/") || strings.Contains(arg, "\\") || arg == "." || arg == ".." {
				fmt.Fprintf(os.Stderr, "%s[Safety] %v%s\n", ui.ColorRed, err, ui.ColorReset)
				return false, fmt.Sprintf("path safety check failed for argument '%s': %v", arg, err)
			}
		}
	}
	return true, ""
}
