// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_config "github.com/gosharplite/tell-me-go/internal/domain/config"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// AppDependencies encapsulates all dependencies for the CLI application.
type AppDependencies struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	HomeDir      string
	SM           domain_security.Manager
	Logger       *slog.Logger
	Bootstrapper Bootstrapper
	ConfigLoader domain_config.ConfigLoader
	ChatService  agent.ChatService
}

// app represents the tell-me-go application.
type app struct {
	Version      string
	Stdin        io.Reader
	Stdout       io.Writer
	Stderr       io.Writer
	homeDir      string
	sm           domain_security.Manager
	logger       *slog.Logger
	bootstrapper Bootstrapper
	configLoader domain_config.ConfigLoader
	chatService  agent.ChatService
	mockPrompt   string
	mockAnswer   string
}

// New creates a new App instance with explicit dependency injection.
func New(deps AppDependencies) *app {
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

	return &app{
		Version:      deps.Version,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		homeDir:      deps.HomeDir,
		sm:           deps.SM,
		logger:       deps.Logger,
		bootstrapper: deps.Bootstrapper,
		configLoader: deps.ConfigLoader,
		chatService:  deps.ChatService,
		mockPrompt:   os.Getenv("TELL_ME_MOCK_PROMPT"),
		mockAnswer:   os.Getenv("TELL_ME_MOCK_ANSWER"),
	}
}

// Run executes the application logic.
func (a *app) Run(ctx stdctx.Context, args []string) error {
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

	if a.bootstrapper == nil {
		return errors.New("application bootstrapper is not initialized")
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
