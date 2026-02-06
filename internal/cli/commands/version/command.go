// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package version

import (
	"context"
	"fmt"
	"io"

	"github.com/gosharplite/tell-me-go/internal/cli/command"
)

// Command implements the version command.
type Command struct {
	Version string
	Stdout  io.Writer
}

func init() {
	command.Register("version", func(ctx *command.Context) command.Command {
		return &Command{
			Version: ctx.Version,
			Stdout:  ctx.Stdout,
		}
	})
}

// Execute prints the version information.
func (c *Command) Execute(ctx context.Context, args []string) error {
	fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
	return nil
}
