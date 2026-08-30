//go:build !windows
// +build !windows

package workspace

import (
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/events"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence/persistencetest"
	infra_process "github.com/gosharplite/tell-me-go/internal/infrastructure/process"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
	"github.com/gosharplite/tell-me-go/internal/tools/toolstest"
)

func TestShellTool_ExecuteCommand_Validation_POSIX(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process-spawning test in short mode")
	}
	sm := &toolstest.MockSecurityManager{AllowAll: true}
	sm.SetBypassActive(true)
	tool := newshellTool(sm, &events.NoOpEventBus{}, security.NewCommandValidator(sm, nil), &posixTranslator{}, &posixShellWrapper{}, persistencetest.NewPlainOSFileSystem(), infra_process.NewRunner())
	ctx := context.Background()

	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{
			name:    "Safe command",
			command: "ls -la",
			wantErr: false,
		},
		{
			name:    "Automatic shell for &&",
			command: "ls && echo hi",
			wantErr: false,
		},
		{
			name:    "Automatic shell for ||",
			command: "ls || echo hi",
			wantErr: false,
		},
		{
			name:    "Automatic shell for ;",
			command: "ls ; echo hi",
			wantErr: false,
		},
		{
			name:    "Automatic shell for |",
			command: "ls | grep foo",
			wantErr: false,
		},
		{
			name:    "Automatic shell for >",
			command: "ls > out.txt",
			wantErr: false,
		},
		{
			name:    "Already inside sh -c",
			command: `sh -c "ls && echo hi"`,
			wantErr: false,
		},
		{
			name:    "Operator inside grep pattern",
			command: `grep "foo && bar" file.go`,
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
