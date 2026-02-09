// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"context"
	"fmt"
	"io"
)

// VersionCommand implements the version command.
type VersionCommand struct {
	Version string
	Stdout  io.Writer
}

func init() {
	Register("version", func(ctx *Context) Command {
		return &VersionCommand{
			Version: ctx.Version,
			Stdout:  ctx.Stdout,
		}
	})
}

// Execute prints the version information.
func (c *VersionCommand) Execute(ctx context.Context, args []string) error {
	fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
	return nil
}
