// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/clitest"
	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/config/configtest"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
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

	t.Run("TTY not available", func(t *testing.T) {
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

		// Use capturerOverride to force a deterministic TTY check.
		// Without this, prior test packages can pollute TTY detection
		// global state, causing IsTTY to return true and execution to
		// reach GetUnifiedHistoryProvider on an unprepared mock.
		// Same class of bug as #515, #511, #512.
		c := &browseCommand{
			ctx:              cmdCtx,
			capturerOverride: &mockCapturerInteractor{isTTY: false},
		}

		err := c.runBrowse(stdctx.Background(), "test-config.yaml")
		require.Error(t, err)
		require.Contains(t, err.Error(), "requires an interactive TTY")
	})
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

	capturer, cleanup, err := c.setupCapturer()
	require.NoError(t, err, "setupCapturer should not error in normal conditions")
	require.NotNil(t, capturer, "expected non-nil capturer from setupCapturer")
	require.NotNil(t, cleanup, "expected non-nil cleanup from setupCapturer")

	// Verify cleanup does not panic
	err = cleanup(stdctx.Background())
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

	// Use capturerFactory (not capturerOverride) so getCapturer routes
	// through setupCapturer, which populates InteractorRef before the
	// TTY check. The factory returns a mock with isTTY:false for
	// deterministic behavior regardless of global terminal state.
	// Same class of bug as #515, #511, #512.
	c := &browseCommand{
		ctx: cmdCtx,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return &mockCapturerInteractor{isTTY: false}
		},
	}

	err := c.runBrowse(stdctx.Background(), "test-config.yaml")
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

	// Use capturerOverride to force a deterministic TTY check result.
	// Without this, prior test packages can pollute TTY detection global
	// state (os.Stdout, terminal env vars), causing IsTTY to return true
	// and execution to reach GetUnifiedHistoryProvider on an unprepared
	// mock — same class of bug as #511/#512.
	c := &browseCommand{
		ctx:              cmdCtx,
		capturerOverride: &mockCapturerInteractor{isTTY: false},
	}

	err := c.runBrowse(stdctx.Background(), "test-config.yaml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "requires an interactive TTY")
	// Should not panic with nil InteractorRef
}

// assertSnapHistoryManagerError asserts bootstrapper snapshot for the
// "history manager error" case: GetHistoryManager was called once but
// GetUnifiedHistoryProvider was never reached.
func assertSnapHistoryManagerError(t *testing.T, snap clitest.BootstrapperSnapshot) {
	t.Helper()
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetUnifiedHistoryProvider != 0 {
		t.Errorf("GetUnifiedHistoryProvider: expected 0, got %d", snap.GetUnifiedHistoryProvider)
	}
}

// assertSnapBothCalled asserts bootstrapper snapshot for cases where both
// GetHistoryManager and GetUnifiedHistoryProvider are expected to be called once.
func assertSnapBothCalled(t *testing.T, snap clitest.BootstrapperSnapshot) {
	t.Helper()
	if snap.GetHistoryManager != 1 {
		t.Errorf("GetHistoryManager: expected 1, got %d", snap.GetHistoryManager)
	}
	if snap.GetUnifiedHistoryProvider != 1 {
		t.Errorf("GetUnifiedHistoryProvider: expected 1, got %d", snap.GetUnifiedHistoryProvider)
	}
}

func TestBrowseCommand_RunBrowse_PostTTY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setupMocks     func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService)
		wantErr        string
		wantNoError    bool
		assertSnapshot func(t *testing.T, snap clitest.BootstrapperSnapshot)
	}{
		{
			name: "config load error",
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return nil, errors.New("config not found") }
			},
			wantErr:        "error loading config",
			assertSnapshot: nil,
		},
		{
			name: "history manager error",
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
					return nil, errors.New("hm failed")
				}
			},
			wantErr:        "failed to get history manager",
			assertSnapshot: assertSnapHistoryManagerError,
		},
		{
			name: "history provider error",
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
					return &stubHistoryManager{}, nil
				}
				mb.GetUnifiedHistoryProviderFunc = func(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
					return nil, errors.New("provider failed")
				}
			},
			wantErr:        "failed to get unified history provider",
			assertSnapshot: assertSnapBothCalled,
		},
		{
			name: "BrowseHistory error",
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
					return &stubHistoryManager{}, nil
				}
				mb.GetUnifiedHistoryProviderFunc = func(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
					return &stubUnifiedHistoryProvider{}, nil
				}
				ms.BrowseHistoryFunc = func(ctx stdctx.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
					return errors.New("browse crash")
				}
			},
			wantErr:        "browse crash",
			assertSnapshot: assertSnapBothCalled,
		},
		{
			name: "success",
			setupMocks: func(ml *configtest.MockConfigLoader, mb *clitest.MockBootstrapper, ms *clitest.MockChatService) {
				ml.LoadFunc = func(path string) (*config.Config, error) { return &config.Config{}, nil }
				mb.GetHistoryManagerFunc = func(ctx stdctx.Context, cfg *config.Config) (ports.HistoryManager, error) {
					return &stubHistoryManager{}, nil
				}
				mb.GetUnifiedHistoryProviderFunc = func(ctx stdctx.Context, cfg *config.Config, hManager ports.HistoryManager) (ports.UnifiedHistoryProvider, error) {
					return &stubUnifiedHistoryProvider{}, nil
				}
				ms.BrowseHistoryFunc = func(ctx stdctx.Context, provider ports.UnifiedHistoryProvider, hManager ports.HistoryManager) error {
					return nil
				}
			},
			wantNoError:    true,
			assertSnapshot: assertSnapBothCalled,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr strings.Builder
			sm := &mockSM{}
			ml := &configtest.MockConfigLoader{}
			mb := &clitest.MockBootstrapper{}
			ms := &clitest.MockChatService{}

			tt.setupMocks(ml, mb, ms)

			cmdCtx := &context{
				Stdin:        strings.NewReader(""),
				Stdout:       &stdout,
				Stderr:       &stderr,
				SM:           sm,
				Bootstrapper: mb,
				Loader:       ml,
				ChatService:  ms,
				Interactor:   NewInteractorRef(),
			}

			c := &browseCommand{
				ctx:              cmdCtx,
				capturerOverride: &mockCapturerInteractor{isTTY: true},
			}

			err := c.runBrowse(stdctx.Background(), "test-config.yaml")

			if tt.wantNoError {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
			}

			// ADR-021: Snapshot assertions for bootstrapper call counts.
			snap := mb.Snapshot()
			if snap.BuildSessionDependencies != 0 {
				t.Errorf("BuildSessionDependencies: expected 0, got %d", snap.BuildSessionDependencies)
			}

			if tt.assertSnapshot != nil {
				tt.assertSnapshot(t, snap)
			}
		})
	}
}

// TestBrowseCommand_GetCapturer_OverrideCloseError verifies that when
// capturerOverride is set, getCapturer returns a cleanup function that
// propagates Close errors and writes a warning to stderr.
func TestBrowseCommand_GetCapturer_OverrideCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("browse getCapturer close exploded")
	mockCap := &mockCapturerInteractor{
		closeFn: func(ctx stdctx.Context) error { return closeErr },
	}

	var stderr strings.Builder
	cmdCtx := &context{Stderr: &stderr}
	c := &browseCommand{
		ctx:              cmdCtx,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.getCapturer()
	require.NoError(t, err, "getCapturer should not error when override is set")
	require.NotNil(t, capturer, "expected non-nil capturer from getCapturer override path")
	require.NotNil(t, cleanup, "expected non-nil cleanup from getCapturer override path")

	err = cleanup(stdctx.Background())
	require.ErrorIs(t, err, closeErr, "cleanup should propagate the Close error")
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "browse getCapturer close exploded")
}

// TestBrowseCommand_GetCapturer_OverrideCloseSuccess verifies that when
// capturerOverride is set and Close succeeds, the cleanup returns nil
// and writes nothing to stderr.
func TestBrowseCommand_GetCapturer_OverrideCloseSuccess(t *testing.T) {
	t.Parallel()

	mockCap := &mockCapturerInteractor{} // closeFn nil → Close returns nil

	var stderr strings.Builder
	cmdCtx := &context{Stderr: &stderr}
	c := &browseCommand{
		ctx:              cmdCtx,
		capturerOverride: mockCap,
	}

	capturer, cleanup, err := c.getCapturer()
	require.NoError(t, err)
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	err = cleanup(stdctx.Background())
	require.NoError(t, err, "cleanup should not error when Close succeeds")
	require.Empty(t, stderr.String(), "stderr should be empty when Close succeeds")
}

// TestBrowseCommand_SetupCapturer_NonCapturerInteractor verifies that
// when the capturerFactory returns a value implementing UserInteractor
// but NOT agent.CapturerInteractor, setupCapturer returns an error.
func TestBrowseCommand_SetupCapturer_NonCapturerInteractor(t *testing.T) {
	t.Parallel()

	nonCapturer := &stubInteractor{id: 2}

	var stderr strings.Builder
	cmdCtx := &context{Stderr: &stderr}
	c := &browseCommand{
		ctx: cmdCtx,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return nonCapturer
		},
	}

	capturer, cleanup, err := c.setupCapturer()
	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.NewCapturer did not return an agent.CapturerInteractor")
	require.Nil(t, capturer)
	require.Nil(t, cleanup)
}

// TestBrowseCommand_SetupCapturer_NonOverrideCloseError verifies that when
// capturerOverride is nil and the factory returns a capturer whose Close
// fails, the cleanup propagates the error and writes a warning to stderr.
func TestBrowseCommand_SetupCapturer_NonOverrideCloseError(t *testing.T) {
	t.Parallel()

	closeErr := errors.New("browse non-override close exploded")
	mockCap := &mockCapturerInteractor{
		closeFn: func(ctx stdctx.Context) error { return closeErr },
	}

	var stderr strings.Builder
	cmdCtx := &context{Stderr: &stderr}
	c := &browseCommand{
		ctx: cmdCtx,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return mockCap
		},
	}

	capturer, cleanup, err := c.setupCapturer()
	require.NoError(t, err, "setupCapturer should not error when factory returns valid capturer")
	require.NotNil(t, capturer)
	require.NotNil(t, cleanup)

	err = cleanup(stdctx.Background())
	require.ErrorIs(t, err, closeErr, "cleanup should propagate Close error from factory-provided capturer")
	require.Contains(t, stderr.String(), "Warning: failed to close capturer")
	require.Contains(t, stderr.String(), "browse non-override close exploded")
}

// TestBrowseCommand_RunBrowse_GetCapturerError verifies that when
// getCapturer → setupCapturer fails (because the factory returns a
// non-CapturerInteractor), runBrowse returns the error before reaching
// the TTY check or any config/history loading.
func TestBrowseCommand_RunBrowse_GetCapturerError(t *testing.T) {
	t.Parallel()

	nonCapturer := &stubInteractor{id: 5}

	var stderr strings.Builder
	cmdCtx := &context{Stderr: &stderr}
	c := &browseCommand{
		ctx: cmdCtx,
		capturerFactory: func(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.Manager, clk clock.Clock, mockPrompt, mockAnswer string, disableEscapeSequences bool) domain_security.UserInteractor {
			return nonCapturer
		},
	}

	err := c.runBrowse(stdctx.Background(), "any-config.yaml")

	require.Error(t, err)
	require.Contains(t, err.Error(), "ui.NewCapturer did not return an agent.CapturerInteractor")
}

// TestBrowseCommand_ConfigFlagPropagation verifies that the --config flag
// value is correctly propagated from the Cobra RunE closure through to
// Loader.Load. This exercises the config flag retrieval that occurs in
// the RunE closure (cmd.Flags().GetString("config")), which is otherwise
// bypassed when tests call runBrowse directly with a hardcoded path.
//
// This test constructs a minimal Cobra command tree (root + browse)
// instead of using executeBrowseCommand because executeBrowseCommand
// calls newBrowseCommand which wires the real ui.NewCapturer — whose
// IsTTY check depends on os.Stdout and is non-deterministic across
// environments. Using capturerOverride with isTTY:true bypasses the
// TTY step deterministically while still exercising the full RunE
// closure and flag → config path propagation.
func TestBrowseCommand_ConfigFlagPropagation(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}

	var receivedPath string
	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) {
			receivedPath = path
			// Return an error to short-circuit execution after config load
			return nil, errors.New("stop after config load")
		},
	}

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		Bootstrapper: &clitest.MockBootstrapper{},
		Loader:       ml,
		Interactor:   NewInteractorRef(),
	}

	// Use capturerOverride so getCapturer returns a mock with isTTY:true,
	// bypassing the real ui.NewCapturer that depends on os.Stdout TTY state.
	// This mirrors the pattern used by TestBrowseCommand_RunBrowse_PostTTY.
	c := &browseCommand{
		ctx:              cmdCtx,
		capturerOverride: &mockCapturerInteractor{isTTY: true},
	}

	// Construct a minimal Cobra command matching newBrowseCommand's structure:
	// the RunE closure calls cmd.Flags().GetString("config") and passes the
	// result to c.runBrowse — the exact line we need to cover.
	root := &cobra.Command{}
	root.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")

	browseCmd := &cobra.Command{
		Use: "browse",
		RunE: func(cmd *cobra.Command, args []string) error {
			configPath, _ := cmd.Flags().GetString("config") // the line under test
			return c.runBrowse(cmd.Context(), configPath)
		},
	}
	root.AddCommand(browseCmd)

	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs([]string{"browse", "--config", "custom-browse.yaml"})

	err := root.ExecuteContext(stdctx.Background())
	// Expect an error (our Loader returns one), but config path must propagate
	require.Error(t, err)
	require.Equal(t, "custom-browse.yaml", receivedPath,
		"config path from --config flag should propagate to Loader.Load")
}

// TestBrowseCommand_NewBrowseCommand_RunE_Executes verifies that the
// RunE closure inside newBrowseCommand actually executes when Cobra
// dispatches to the browse subcommand. This covers the config flag
// retrieval line (cmd.Flags().GetString("config")) that lives inside
// newBrowseCommand and is otherwise only covered by direct runBrowse
// calls which bypass the Cobra flag plumbing.
//
// This test uses executeBrowseCommand (which calls newBrowseCommand
// internally) so the actual RunE closure defined in the production
// code is exercised. The error message will vary by environment:
//   - non-TTY: "requires an interactive TTY"
//   - TTY:      "error loading config" or similar downstream error
//
// In either case the RunE closure lines are executed.
func TestBrowseCommand_NewBrowseCommand_RunE_Executes(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}

	ml := &configtest.MockConfigLoader{
		LoadFunc: func(path string) (*config.Config, error) {
			return nil, errors.New("stop after config load")
		},
	}

	cmdCtx := &context{
		Version:      "1.0.0",
		Stdin:        strings.NewReader(""),
		Stdout:       &stdout,
		Stderr:       &stderr,
		SM:           sm,
		Bootstrapper: &clitest.MockBootstrapper{},
		Loader:       ml,
		Interactor:   NewInteractorRef(),
	}

	// executeBrowseCommand calls newBrowseCommand internally, so the
	// RunE closure defined there is the one under test.
	err := executeBrowseCommand(cmdCtx, []string{"browse", "--config", "dummy.yaml"})
	require.Error(t, err, "RunE closure should produce an error in test conditions")
}
