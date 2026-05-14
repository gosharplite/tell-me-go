// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/require"
)

// executeBrowseCommand creates a root command with browse subcommand and executes it.
func executeBrowseCommand(cmdCtx *context, args []string) error {
	root := &cobra.Command{}
	root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")

	chatCmd := newChatCommand(cmdCtx, nil)
	browseCmd := newBrowseCommand(cmdCtx)

	root.AddCommand(chatCmd, browseCmd)

	// Set root RunE to chatCmd.RunE (matching App.Run behavior)
	root.RunE = chatCmd.RunE
	root.Args = cobra.ArbitraryArgs

	chatCmd.Flags().VisitAll(func(f *pflag.Flag) {
		root.Flags().AddFlag(f)
	})

	root.SetOut(cmdCtx.Stdout)
	root.SetErr(cmdCtx.Stderr)

	sanitized := sanitizeArgs(append([]string{"dummy"}, args...))
	root.SetArgs(sanitized[1:])

	return root.ExecuteContext(stdctx.Background())
}

func TestBrowseCommand_RunBrowse_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		setupMocks func(*chatMockLoader, *mockBootstrapper)
		wantErr    string
	}{
		{
			name: "TTY not available",
			setupMocks: func(ml *chatMockLoader, mb *mockBootstrapper) {
				// No mocks needed — capturer.IsTTY(os.Stdout) is false in tests,
				// so the function returns early before touching config/history.
			},
			wantErr: "requires an interactive TTY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			sm := &mockSM{}
			mb, ml, _ := setupMocks()

			if tt.setupMocks != nil {
				tt.setupMocks(ml, mb)
			}

			cmdCtx := &context{
				Version:      "1.0.0",
				Stdin:        strings.NewReader(""),
				Stdout:       &stdout,
				Stderr:       &stderr,
				SM:           sm,
				Bootstrapper: mb,
				Loader:       ml,
			}

			err := executeBrowseCommand(cmdCtx, []string{"browse"})
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestBrowseCommand_SetupCapturer(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}

	cmdCtx := &context{
		Version: "1.0.0",
		Stdin:   strings.NewReader(""),
		Stdout:  &stdout,
		Stderr:  &stderr,
		SM:      sm,
	}

	c := &browseCommand{ctx: cmdCtx}

	capturer, cleanup := c.setupCapturer()
	require.NotNil(t, capturer, "expected non-nil capturer from setupCapturer")
	require.NotNil(t, cleanup, "expected non-nil cleanup from setupCapturer")

	// Verify cleanup does not panic
	err := cleanup(stdctx.Background())
	require.NoError(t, err, "cleanup should not error")
}

func TestBrowseCommand_NewBrowseCommand_RunE(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, _ := setupMocks()

	ref := NewInteractorRef()
	require.Nil(t, ref.Get(), "interactor ref should be nil before command runs")

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		Bootstrapper: mb,
		Loader:       ml,
		Interactor:   ref,
	}

	// RunE should fail with TTY error, but the capturer should have been set
	err := executeBrowseCommand(cmdCtx, []string{"browse"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an interactive TTY")

	// InteractorRef should be populated even though the command errored
	// because setupCapturer runs before the TTY check
	require.NotNil(t, ref.Get(), "interactor ref should be populated after browse command run")
}

func TestBrowseCommand_NewBrowseCommand_HelpFlag(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, _ := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		Bootstrapper: mb,
		Loader:       ml,
	}

	// --help after subcommand triggers help for that subcommand
	err := executeBrowseCommand(cmdCtx, []string{"browse", "--help"})
	require.NoError(t, err)
	require.Contains(t, stdout.String(), "browse")
}

func TestBrowseCommand_NilInteractorRef_DoesNotPanic(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mb, ml, _ := setupMocks()

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		Bootstrapper: mb,
		Loader:       ml,
		Interactor:   nil, // explicit nil
	}

	err := executeBrowseCommand(cmdCtx, []string{"browse"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an interactive TTY")
	// Should not panic with nil InteractorRef
}
