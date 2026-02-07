// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/gosharplite/tell-me-go/internal/cli/command"
	"github.com/gosharplite/tell-me-go/internal/security"
)

// App represents the tell-me-go application.
type App struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	homeDir string
	sm      *security.SecurityManager
}

// New creates a new App instance with default IO and factories.
func New(version string, stdin io.Reader, stdout io.Writer, stderr io.Writer) *App {
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	sm := security.NewSecurityManager(stdin)

	return &App{
		Version: version,
		Stdin:   stdin,
		Stdout:  stdout,
		Stderr:  stderr,
		homeDir: homeDir,
		sm:      sm,
	}
}

// Run executes the application logic.
func (a *App) Run(ctx context.Context, args []string) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	// Determine which command to run
	cmdName := "chat"
	for _, arg := range args {
		if arg == "-v" || arg == "--version" {
			cmdName = "version"
			break
		}
	}

	factory, err := command.Get(cmdName)
	if err != nil {
		return err
	}

	cmdCtx := &command.Context{
		Version: a.Version,
		Stdin:   a.Stdin,
		Stdout:  a.Stdout,
		Stderr:  a.Stderr,
		HomeDir: a.homeDir,
		SM:      a.sm,
	}

	cmd := factory(cmdCtx)

	if err := cmd.Execute(ctx, args); err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}
