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
	"strings"
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

	chatCmd := newChatCommand(cmdCtx)
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

	rootCmd.SetOut(a.Stdout)
	rootCmd.SetErr(a.Stderr)

	// Share chat flags with the root command
	chatCmd.Flags().VisitAll(func(f *pflag.Flag) {
		rootCmd.Flags().AddFlag(f)
	})

	rootCmd.AddCommand(chatCmd, browseCmd, envCmd, versionCmd)

	// Since App.Run receives os.Args, we skip the first element (binary name)
	actualArgs := []string{}
	if len(args) > 1 {
		actualArgs = args[1:]
		if len(actualArgs) > 0 {
			isSubcommand := false
			for _, sub := range rootCmd.Commands() {
				if sub.Name() == actualArgs[0] || sub.HasAlias(actualArgs[0]) {
					isSubcommand = true
					break
				}
			}
			// If not a subcommand and not a flag, default to 'chat'
			if !isSubcommand && !strings.HasPrefix(actualArgs[0], "-") {
				actualArgs = append([]string{"chat"}, actualArgs...)
			}
		}
	}
	rootCmd.SetArgs(actualArgs)

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		if errors.Is(err, stdctx.Canceled) {
			_, _ = fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}
