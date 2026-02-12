// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"fmt"
	"io"
)

// versionCommand implements the version command.
type versionCommand struct {
	Version string
	Stdout  io.Writer
}

func init() {
	register("version", func(ctx *context) command {
		return &versionCommand{
			Version: ctx.Version,
			Stdout:  ctx.Stdout,
		}
	})
}

// Execute prints the version information.
func (c *versionCommand) Execute(ctx stdctx.Context, args []string) error {
	fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
	return nil
}
