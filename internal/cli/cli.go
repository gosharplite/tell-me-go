// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	// errMissingDependency is returned when a required dependency is not provided to New().
	errMissingDependency = errors.New("missing required dependency")
)

// AppDependencies encapsulates all dependencies for the CLI application.
type AppDependencies struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	HomeDir      string
	SM           domain_security.Manager
	Bootstrapper Bootstrapper
	ConfigLoader domain_config.ConfigLoader
	ChatService  agent.ChatService
}

// App represents the tell-me-go application.
type App struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	homeDir      string
	sm           domain_security.Manager
	bootstrapper Bootstrapper
	configLoader domain_config.ConfigLoader
	chatService  agent.ChatService
	mockPrompt   string
	mockAnswer   string
}

// New creates a new App instance with explicit dependency injection.
func New(deps AppDependencies, getenv func(string) string) (*App, error) {
	if deps.Bootstrapper == nil {
		return nil, fmt.Errorf("%w: Bootstrapper", errMissingDependency)
	}
	if deps.SM == nil {
		return nil, fmt.Errorf("%w: SM", errMissingDependency)
	}
	if deps.ConfigLoader == nil {
		return nil, fmt.Errorf("%w: ConfigLoader", errMissingDependency)
	}
	if deps.ChatService == nil {
		return nil, fmt.Errorf("%w: ChatService", errMissingDependency)
	}

	stdin := deps.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := deps.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := deps.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return &App{
		Version:      deps.Version,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		homeDir:      deps.HomeDir,
		sm:           deps.SM,
		bootstrapper: deps.Bootstrapper,
		configLoader: deps.ConfigLoader,
		chatService:  deps.ChatService,
		mockPrompt:   getenv("TELL_ME_MOCK_PROMPT"),
		mockAnswer:   getenv("TELL_ME_MOCK_ANSWER"),
	}, nil
}

// Run executes the application logic.
func (a *App) Run(ctx stdctx.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Ensure the security manager (and underlying audit log file) flushes and closes on exit
	if a.sm != nil {
		defer func() {
			_ = a.sm.Close()
		}()
	}

	cmdCtx := &context{
		Version:      a.Version,
		Stdin:        a.Stdin,
		Stdout:       a.Stdout,
		Stderr:       a.Stderr,
		HomeDir:      a.homeDir,
		SM:           a.sm,
		ChatService:  a.chatService,
		Bootstrapper: a.bootstrapper,
		Loader:       a.configLoader,
		MockPrompt:   a.mockPrompt,
		MockAnswer:   a.mockAnswer,
	}

	chatCmd := newChatCommand(cmdCtx, nil)
	browseCmd := newBrowseCommand(cmdCtx)
	envCmd := newEnvCommand(cmdCtx)
	versionCmd := newVersionCommand(cmdCtx)

	rootCmd := &cobra.Command{
		Use:   "tell-me-go [prompt]",
		Short: "AI-powered CLI assistant",
		Long:  `tell-me-go is a CLI tool that lets you interact with LLMs directly from your terminal.`,
		// The root command behaves like 'chat' if no subcommand matches
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE:          chatCmd.RunE,
		Version:       a.Version,
		Args:          cobra.ArbitraryArgs,
	}

	rootCmd.PersistentFlags().StringP("config", "c", "configs/assistant.yaml", "Path to the configuration file")

	rootCmd.SetOut(a.Stdout)
	rootCmd.SetErr(a.Stderr)

	// Since rootCmd and chatCmd share the same RunE logic (which uses chatCmd's internal opts),
	// we just need to make sure both commands have the same flags defined.
	// Re-binding the same flags to the same opts object is handled inside newChatCommand.
	// For the root command, we can just share the flags.
	chatCmd.Flags().VisitAll(func(f *pflag.Flag) {
		rootCmd.Flags().AddFlag(f)
	})

	rootCmd.AddCommand(chatCmd, browseCmd, envCmd, versionCmd)

	// Since App.Run receives os.Args, we sanitize them first
	args = sanitizeArgs(args)

	// Since App.Run receives os.Args, we skip the first element (binary name)
	if len(args) > 1 {
		rootCmd.SetArgs(args[1:])
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, stdctx.Canceled) {
			_, _ = fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}

// sanitizeArgs preprocessing allows integer flags like -l and -b to behave like boolean flags
// defaulting to 1 when no argument is explicitly provided, preventing positional arguments
// (like the prompt string) from being mistakenly parsed as their values.
func sanitizeArgs(args []string) []string {
	if len(args) < 2 {
		return args
	}

	result := make([]string, 0, len(args))
	result = append(result, args[0])

	for i := 1; i < len(args); i++ {
		arg := args[i]
		result = append(result, arg)

		if arg == "-l" || arg == "--last" || arg == "-b" || arg == "--back" {
			isNextNum := false
			if i+1 < len(args) {
				// ParseInt is robust enough to know if the next string is a base-10 number
				if _, err := strconv.Atoi(args[i+1]); err == nil {
					isNextNum = true
				}
			}
			if !isNextNum {
				result = append(result, "1")
			}
		}
	}
	return result
}
