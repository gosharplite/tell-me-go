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
	mockPrompt string
	mockAnswer string
}

// New creates a new App instance with default IO and factories.
func New(version string, stdin io.Reader, stdout io.Writer, stderr io.Writer) *app {
	homeDir := os.Getenv("TELL_ME_HOME")
	if homeDir == "" {
		homeDir = os.Getenv("AIT_HOME")
	}
	if homeDir == "" {
		homeDir = "."
	}

	sm := security.NewSecurityManager(nil)

	return &app{
		Version:    version,
		Stdin:      stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		homeDir:    homeDir,
		sm:         sm,
		mockPrompt: os.Getenv("TELL_ME_MOCK_PROMPT"),
		mockAnswer: os.Getenv("TELL_ME_MOCK_ANSWER"),
	}
}

// Run executes the application logic.
func (a *app) Run(ctx stdctx.Context, args []string) error {
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

	factory, err := get(cmdName)
	if err != nil {
		return err
	}

	cmdCtx := &context{
		Version:    a.Version,
		Stdin:      a.Stdin,
		Stdout:     a.Stdout,
		Stderr:     a.Stderr,
		HomeDir:    a.homeDir,
		SM:         a.sm,
		MockPrompt: a.mockPrompt,
		MockAnswer: a.mockAnswer,
	}

	cmd := factory(cmdCtx)

	if err := cmd.Execute(ctx, args); err != nil {
		if errors.Is(err, stdctx.Canceled) {
			fmt.Fprintln(a.Stderr)
			return nil
		}
		return err
	}
	return nil
}
