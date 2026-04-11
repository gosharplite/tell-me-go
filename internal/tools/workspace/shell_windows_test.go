//go:build windows
// +build windows

package workspace

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

func TestShellTool_ExecuteCommand_Validation_Windows(t *testing.T) {
	sm := security.NewSecurityManager(nil)
	sm.SetBypassActive(true)
	tool := newshellTool(sm, security.NewCommandValidator(sm, nil), &windowsTranslator{}, &windowsShellWrapper{})
	ctx := context.Background()

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Safe command",
			command: "dir",
			wantErr: false,
		},
		{
			name:    "PowerShell cmdlet",
			command: "Get-ChildItem",
			wantErr: false,
		},
		{
			name:    "Automatic shell for ;",
			command: "dir ; echo hi",
			wantErr: false,
		},
		{
			name:    "Automatic shell for |",
			command: "dir | Select-String foo",
			wantErr: false,
		},
		{
			name:    "Automatic shell for >",
			command: "dir > out.txt",
			wantErr: false,
		},
		{
			name:    "Already inside powershell",
			command: `powershell -Command "dir; echo hi"`,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.ExecuteCommand(ctx, map[string]interface{}{
				"command": tt.command,
				"reason":  "testing validation",
			}, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("ExecuteCommand(%q) error = %v, wantErr %v", tt.command, err, tt.wantErr)
			}
		})
	}
}
