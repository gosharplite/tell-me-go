// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package framework

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
)

func TestCommandValidator(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.RegisterReadOnlyPath("/etc")
	v := NewCommandValidator(sm)

	tests := []struct {
		cmd     string
		allowed bool
	}{
		{"ls -la", true},
		{"rm -rf /", false},
		{"ls; rm -rf /", false},
		{"ls | grep foo", false},
		{"cat /etc/passwd", true},
		{"cat /etc/shadow", true},
		{"git status", true},
		{"git push", false},
	}

	for _, tt := range tests {
		allowed, _ := v.IsSafe(tt.cmd)
		if allowed != tt.allowed {
			t.Errorf("IsSafe(%q): allowed=%v, got %v", tt.cmd, tt.allowed, allowed)
		}
	}
}

func TestCommandValidator_ValidateStructure(t *testing.T) {
	v := NewCommandValidator(nil)

	tests := []struct {
		cmd     string
		wantErr bool
	}{
		{"ls -la", false},
		{"ls && echo hi", true},
		{"ls || echo hi", true},
		{"ls ; echo hi", true},
		{"ls | grep foo", true},
		{"ls > out.txt", true},
		{"ls >> out.txt", true},
		{"cat < in.txt", true},
		{"sleep 10 &", true},
		{"grep \"foo && bar\" file.go", false}, // Contains operator but not standalone
		{"sh -c \"ls && echo hi\"", false},      // Operator is inside another string
	}

	for _, tt := range tests {
		parts, err := v.Split(tt.cmd)
		if err != nil {
			t.Errorf("Split(%q) error: %v", tt.cmd, err)
			continue
		}
		err = v.ValidateStructure(parts)
		if (err != nil) != tt.wantErr {
			t.Errorf("ValidateStructure(%q) error = %v, wantErr %v", tt.cmd, err, tt.wantErr)
		}
	}
}
