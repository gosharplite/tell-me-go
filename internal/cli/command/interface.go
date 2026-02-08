// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package command

import (
	"context"
	"io"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// Context provides shared dependencies for commands.
type Context struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	HomeDir string
	SM      *security.SecurityManager
}

// Command represents a CLI command that can be executed.
type Command interface {
	Execute(ctx context.Context, args []string) error
}

// Factory is a function that creates a Command.
type Factory func(ctx *Context) Command
