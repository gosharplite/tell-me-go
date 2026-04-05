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
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
)

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
	mockPrompt   string
	mockAnswer   string
}

// New creates a new App instance with explicit dependency injection.
func New(version string, stdin io.Reader, stdout, stderr io.Writer, b Bootstrapper, sm domain_security.Manager, homeDir string, logger *slog.Logger) *app {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}

	return &app{
		Version:      version,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		homeDir:      homeDir,
		sm:           sm,
		logger:       logger,
		bootstrapper: b,
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

	loader := &config.YAMLConfigLoader{}
	if a.bootstrapper == nil {
		return errors.New("application bootstrapper is not initialized")
	}

	chatService := agent.NewChatService(
		a.homeDir, a.Version, a.Stdout, a.Stderr, a.sm,
		a.bootstrapper,
		a.bootstrapper.GetAgentFactory(),
		a.bootstrapper.GetUIRenderer(),
		a.bootstrapper.GetHistoryRenderer(),
		a.bootstrapper.GetHistoryBrowser(),
	)

	cmdCtx := &context{
		Version:      a.Version,
		Stdin:        a.Stdin,
		Stdout:       a.Stdout,
		Stderr:       a.Stderr,
		HomeDir:      a.homeDir,
		SM:           a.sm,
		ChatService:  chatService,
		Bootstrapper: a.bootstrapper,
		Loader:       loader,
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
