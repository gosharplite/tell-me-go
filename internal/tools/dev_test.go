// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"testing"
)

func TestRunTestsVulnerability(t *testing.T) {
	sm := NewSecurityManager()
	m := &devManager{sm: sm}
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
			command: "go test ./internal/config; echo 'pwned'",
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
