// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/agent"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

// context provides shared dependencies for commands.
type context struct {
	Version     string
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	HomeDir     string
	SM          domain_security.Manager
	ChatService agent.ChatService
	MockPrompt  string
	MockAnswer  string
}

// command represents a CLI command that can be executed.
type command interface {
	Execute(ctx stdctx.Context, args []string) error
}

// factory is a function that creates a command.
type factory func(ctx *context) command
