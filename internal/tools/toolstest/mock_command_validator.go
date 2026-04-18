// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package toolstest

import (
	"errors"
	"strings"
)

// MockCommandValidator is a test double for security.CommandValidator.
// Each method delegates to its corresponding *Func field when set;
// otherwise sensible defaults apply (IsSafe returns true; Split uses
// strings.Fields with naive unclosed-quote detection;
// ValidateStructure/CheckPathSafety pass; HasShellFeatures uses a
// PowerShell-cmdlet/$variable heuristic).
type MockCommandValidator struct {
	IsSafeFunc            func(command string) (bool, string)
	SplitFunc             func(cmd string) ([]string, error)
	ValidateStructureFunc func(parts []string) error
	CheckPathSafetyFunc   func(parts []string) (bool, string)
	HasShellFeaturesFunc  func(parts []string) bool
}

func (m *MockCommandValidator) IsSafe(command string) (bool, string) {
	if m.IsSafeFunc != nil {
		return m.IsSafeFunc(command)
	}
	return true, ""
}

func (m *MockCommandValidator) Split(cmd string) ([]string, error) {
	if m.SplitFunc != nil {
		return m.SplitFunc(cmd)
	}
	// Simple split for mock, but detect unclosed quotes for tests
	if strings.Count(cmd, "'")%2 != 0 || strings.Count(cmd, "\"")%2 != 0 {
		return nil, errors.New("unclosed quote")
	}
	return strings.Fields(cmd), nil
}

func (m *MockCommandValidator) ValidateStructure(parts []string) error {
	if m.ValidateStructureFunc != nil {
		return m.ValidateStructureFunc(parts)
	}
	return nil
}

func (m *MockCommandValidator) CheckPathSafety(parts []string) (bool, string) {
	if m.CheckPathSafetyFunc != nil {
		return m.CheckPathSafetyFunc(parts)
	}
	return true, ""
}

func (m *MockCommandValidator) HasShellFeatures(parts []string) bool {
	if m.HasShellFeaturesFunc != nil {
		return m.HasShellFeaturesFunc(parts)
	}
	// Default heuristic for PowerShell cmdlets
	for _, p := range parts {
		if (len(p) > 3 && p[0] >= 'A' && p[0] <= 'Z' && containsHyphen(p)) ||
			(len(p) > 1 && p[0] == '$') {
			return true
		}
	}
	return false
}

func containsHyphen(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '-' {
			return true
		}
	}
	return false
}
