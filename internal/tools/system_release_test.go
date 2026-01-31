// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tools

import (
	"context"
	"path/filepath"
	"testing"
)

func TestExecuteCommand_Security(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.SetBypassFile(filepath.Join(tmpDir, "bypass.log"))
	
	m := &systemManager{sm: sm}

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{"Forbidden Shell Char", "ls; cat /etc/passwd", true},
		{"Forbidden Shell Char Pipe", "ls | grep go", true},
		{"Safe Command", "ls", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{"command": tt.command}
			_, err := m.executeCommand(context.Background(), args)
			if (err != nil) != tt.wantErr {
				t.Errorf("executeCommand() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestExecuteCommand_Execution(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.SetBypassFile(filepath.Join(tmpDir, "bypass.log"))
	
	// Authorize
	sm.bypassConfirmations = true

	m := &systemManager{sm: sm}

	args := map[string]interface{}{"command": "echo hello"}
	resp, err := m.executeCommand(context.Background(), args)
	if err != nil {
		t.Fatalf("executeCommand failed: %v", err)
	}

	if resp.Text != "hello\n" {
		t.Errorf("expected 'hello\n', got %q", resp.Text)
	}
}

func TestPipeCommands_Security(t *testing.T) {
	tmpDir := t.TempDir()
	sm := NewSecurityManager()
	sm.SetBypassFile(filepath.Join(tmpDir, "bypass.log"))
	
	m := &systemManager{sm: sm}

	tests := []struct {
		name     string
		commands []string
		wantErr  bool
	}{
		{"Single Command Pipe", []string{"ls"}, false},
		{"Forbidden Char in Pipe", []string{"ls", "grep ;"}, true},
		{"Safe Pipe", []string{"ls", "grep go"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := map[string]interface{}{"commands": tt.commands}
			_, err := m.pipeCommands(context.Background(), args)
			if (err != nil) != tt.wantErr {
				t.Errorf("pipeCommands() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
