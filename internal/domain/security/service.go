// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import (
	"strings"
)

// SafetyService provides domain-level security logic.
type SafetyService struct {
	policy *Policy
}

// NewSafetyService creates a new SafetyService.
func NewSafetyService(policy *Policy) *SafetyService {
	return &SafetyService{policy: policy}
}

// IsCommandSafe checks if a command is safe for auto-approval based on the policy.
func (s *SafetyService) IsCommandSafe(cmd string) (bool, string) {
	if !s.policy.IsCommandAllowed(cmd) {
		return false, "command not allowed by policy"
	}
	if !s.policy.isAutoApprovable(cmd) {
		return false, "command requires manual confirmation"
	}
	return true, ""
}

// HasForbiddenOperators checks for forbidden shell operators in the command parts.
func (s *SafetyService) HasForbiddenOperators(parts []string) (bool, string) {
	operators := map[string]string{
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

	for _, part := range parts {
		if desc, found := operators[part]; found {
			return true, desc
		}
	}
	return false, ""
}

// HasUnsafeInterpolation checks for shell interpolation characters.
func (s *SafetyService) HasUnsafeInterpolation(part string) bool {
	return strings.ContainsAny(part, "$`")
}

// HasForbiddenCharsInCommand checks for forbidden characters in the command token.
func (s *SafetyService) HasForbiddenCharsInCommand(cmd string) bool {
	return strings.ContainsAny(cmd, ";&|><\n\r")
}

// IsSafeGitSubcommand checks if a git subcommand is safe for auto-approval.
func (s *SafetyService) IsSafeGitSubcommand(sub string) bool {
	return s.policy.SafeGitSubcommands[sub]
}

// IsSafeGoSubcommand checks if a go subcommand is safe for auto-approval.
func (s *SafetyService) IsSafeGoSubcommand(sub string) bool {
	return s.policy.SafeGoSubcommands[sub]
}

// IsSafeGhSubcommand checks if a gh subcommand is safe for auto-approval.
func (s *SafetyService) IsSafeGhSubcommand(sub string) bool {
	return s.policy.SafeGhSubcommands[sub]
}
