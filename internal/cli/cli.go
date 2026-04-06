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
	"syscall"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	infra_persistence "github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
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
	FileSystem   infra_persistence.FileSystem
}

// App represents the tell-me-go application.
type App struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	HomeDir      string
	sm           domain_security.Manager
	bootstrapper Bootstrapper
	configLoader domain_config.ConfigLoader
	chatService  agent.ChatService
	fileSystem   infra_persistence.FileSystem
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

	fileSystem := deps.FileSystem
	if fileSystem == nil {
		fileSystem = &infra_persistence.OSFileSystem{}
	}

	return &App{
		Version:      deps.Version,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		HomeDir:      deps.HomeDir,
		sm:           deps.SM,
		bootstrapper: deps.Bootstrapper,
		configLoader: deps.ConfigLoader,
		chatService:  deps.ChatService,
		fileSystem:   fileSystem,
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

	// Determine which command to run
	cmdName := "chat"
	for _, arg := range args {
		if arg == "browse" {
			cmdName = "browse"
			break
		}
		if arg == "-v" || arg == "--version" {
			cmdName = "version"
			break
		}
	}

	factory, err := get(cmdName)
	if err != nil {
		return err
	}

	cmdCtx := &context{
		Version:      a.Version,
		Stdin:        a.Stdin,
		Stdout:       a.Stdout,
		Stderr:       a.Stderr,
		HomeDir:      a.HomeDir,
		SM:           a.sm,
		ChatService:  a.chatService,
		Bootstrapper: a.bootstrapper,
		Loader:       a.configLoader,
		FileSystem:   a.fileSystem,
		MockPrompt:   a.mockPrompt,
		MockAnswer:   a.mockAnswer,
	}

	cmd := factory(cmdCtx)

	if err := cmd.Execute(ctx, args); err != nil {
		if errors.Is(err, stdctx.Canceled) {
			_, _ = fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}
