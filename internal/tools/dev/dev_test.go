// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

func TestRunTestsVulnerability(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass confirmation for tests
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
	}
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Safe command",
			command: "go help",
			wantErr: false,
		},
		{
			name:    "Command injection via semicolon",
			command: "go test ./internal/config ; echo 'pwned'",
			wantErr: true,
		},
		{
			name:    "Command injection via ampersand",
			command: "go test ./internal/config && echo 'pwned'",
			wantErr: true,
		},
		{
			name:    "Command injection via pipe",
			command: "go test ./internal/config | echo 'pwned'",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command})
			if (err != nil) != tt.wantErr {
				t.Errorf("runTests() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRunTests_EdgeCases(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true) // Bypass confirmation for tests
	m := &devManager{
		sm:        sm,
		validator: framework.NewCommandValidator(sm),
	}
	ctx := context.Background()

	t.Run("Empty command", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": ""})
		if err == nil {
			t.Error("expected error for empty command")
		}
	})

	t.Run("Unauthorized tool", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": "rm -rf /"})
		if err == nil {
			t.Error("expected error for unauthorized tool")
		}
	})

	t.Run("Invalid shlex", func(t *testing.T) {
		_, err := m.runTests(ctx, map[string]interface{}{"command": "go test 'unclosed quote"})
		if err == nil {
			t.Error("expected error for invalid shlex")
		}
	})
}
