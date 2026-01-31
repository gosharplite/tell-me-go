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
