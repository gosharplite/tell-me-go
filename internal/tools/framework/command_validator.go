// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"fmt"
	"os"
	"strings"

	"github.com/google/shlex"
	"github.com/gosharplite/tell-me-go/internal/security"
)

// CommandValidator handles command validation and security checks.
type CommandValidator struct {
	sm *security.SecurityManager
}

// NewCommandValidator creates a new CommandValidator.
func NewCommandValidator(sm *security.SecurityManager) *CommandValidator {
	return &CommandValidator{sm: sm}
}

var autoApprovableCommands = map[string]bool{
	"grep": true, "ls": true, "pwd": true, "cat": true, "echo": true,
	"head": true, "tail": true, "wc": true, "stat": true, "date": true,
	"whoami": true, "diff": true, "git": true, "go": true,
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
	base := parts[0]

	// 1. Check against central security policy whitelist (Single Source of Truth)
	if !v.sm.IsCommandAllowed(base) {
		return false, fmt.Sprintf("command '%s' is not allowed by security policy", base)
	}

	// 2. Check if the command is side-effect-free (inspection only) for auto-approval.
	if !autoApprovableCommands[base] {
		return false, fmt.Sprintf("command '%s' is not in the auto-approval whitelist", base)
	}

	// 3. Specialized Check for 'git'
	if base == "git" {
		if safe, reason := v.isSafeGit(parts); !safe {
			return false, reason
		}
	}

	// 4. Specialized check for 'go'
	if base == "go" {
		if safe, reason := v.isSafeGo(parts); !safe {
			return false, reason
		}
	}

	// 5. Check for unsafe characters (pipes, redirects, expansion, etc.)
	if safe, reason := v.hasUnsafeChars(command); !safe {
		return false, reason
	}

	// 6. Path Safety Check: Ensure all arguments stay within allowed boundaries.
	if safe, reason := v.checkPathSafety(parts); !safe {
		return false, reason
	}

	return true, ""
}

// Split uses shlex to split a command string into arguments.
func (v *CommandValidator) Split(cmd string) ([]string, error) {
	parts, err := shlex.Split(cmd)
	if err != nil {
		return nil, err
	}
	return parts, nil
}

func (v *CommandValidator) isSafeGit(parts []string) (bool, string) {
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
		return false, fmt.Sprintf("go subcommand '%s' is not in the safe whitelist", sub)
	}
	return true, ""
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

func (v *CommandValidator) checkPathSafety(parts []string) (bool, string) {
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
				fmt.Fprintf(os.Stderr, "\033[0;31m[Safety] %v\033[0m\n", err)
				return false, fmt.Sprintf("path safety check failed for argument '%s': %v", arg, err)
			}
		}
	}
	return true, ""
}
