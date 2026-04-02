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
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// app represents the tell-me-go application.
type app struct {
	Version    string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	homeDir    string
	sm         *security.SecurityManager
	logger     *slog.Logger
	mockPrompt string
	mockAnswer string
}

// New creates a new App instance with default IO and factories.
func New(version string, stdin io.Reader, stdout, stderr io.Writer) (*app, *slog.Logger) {
	if stdin == nil {
		stdin = os.Stdin
	}
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	sm := security.NewSecurityManager(nil)

	logLevel := slog.LevelWarn
	if os.Getenv("TELL_ME_DEBUG") == "1" {
		logLevel = slog.LevelDebug
	}
	logHandler := slog.NewTextHandler(stderr, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(logHandler)

	return &app{
		Version:    version,
		Stdin:      stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		homeDir:    homeDir,
		sm:         sm,
		logger:     logger,
		mockPrompt: os.Getenv("TELL_ME_MOCK_PROMPT"),
		mockAnswer: os.Getenv("TELL_ME_MOCK_ANSWER"),
	}, logger
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

	// Assembly root: wire dependencies for orchestration
	container := di.NewBootstrapper(a.homeDir, a.sm, a.Version, a.Stdout, a.Stderr, a.logger, nil)
	loader := &config.YAMLConfigLoader{}
	chatService := agent.NewChatService(a.homeDir, a.Version, a.Stdout, a.Stderr, a.sm, loader, container)

	cmdCtx := &context{
		Version:     a.Version,
		Stdin:       a.Stdin,
		Stdout:      a.Stdout,
		Stderr:      a.Stderr,
		HomeDir:     a.homeDir,
		SM:          a.sm,
		ChatService: chatService,
		MockPrompt:  a.mockPrompt,
		MockAnswer:  a.mockAnswer,
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
