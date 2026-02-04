// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package dev

import (
	"context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/security"
	"github.com/gosharplite/tell-me-go/internal/tools/framework"
)

func TestRunTestsVulnerability(t *testing.T) {
	sm := security.NewSecurityManager(nil)
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
			command: "go test ./internal/config",
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
			res, err := m.runTests(context.Background(), map[string]interface{}{"command": tt.command})
			// Validation errors are now returned in res.Text with nil err
			isValidationError := err == nil && tt.wantErr && res.Text != "" && (
				strings.Contains(res.Text, "detected") || 
				strings.Contains(res.Text, "violation") ||
				strings.Contains(res.Text, "Error parsing"))
			
			if (err != nil || isValidationError) != tt.wantErr {
				t.Errorf("runTests() error = %v, res.Text = %v, wantErr %v", err, res.Text, tt.wantErr)
			}
		})
	}
}
