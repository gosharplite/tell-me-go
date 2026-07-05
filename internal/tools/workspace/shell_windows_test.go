//go:build windows
// +build windows

package workspace

import (
	"context"
	"runtime"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestShellTool_ExecuteCommand_Validation_Windows(t *testing.T) {
	sm := &toolstest.MockSecurityManager{AllowAll: true}
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

// TestWindowsShellWrapper_BareLFRoutesToPowerShell verifies Phase 4:
// On Windows, a command with embedded bare LF must route through
// PowerShell -Command (via isPowerShellIndicator), not cmd.exe /c,
// because cmd.exe /c silently drops lines after the first LF.
func TestWindowsShellWrapper_BareLFRoutesToPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("bare-LF PowerShell routing is Windows-specific")
	}

	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	v := security.NewCommandValidator(sm, nil)
	tool := newTestShellTool(sm, v)
	ctx := context.Background()

	// A two-line command: both echo statements must produce output.
	// On Windows, cmd.exe /c would only execute the first line.
	res, err := tool.ExecuteCommand(ctx, map[string]interface{}{
		"command": "echo FirstLine\necho SecondLine",
		"reason":  "test windows bare lf powershell routing",
	}, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Error != nil {
		t.Fatalf("unexpected res.Error: %v", res.Error)
	}

	// Both lines must appear in output
	if !strings.Contains(res.Text, "FirstLine") {
		t.Errorf("expected output to contain 'FirstLine', got: %s", res.Text)
	}
	if !strings.Contains(res.Text, "SecondLine") {
		t.Errorf("expected output to contain 'SecondLine', got: %s", res.Text)
	}
}
