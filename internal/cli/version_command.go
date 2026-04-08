// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// versionCommand implements the version command.
type versionCommand struct {
	Version string
	Stdout  io.Writer
}

func newVersionCommand(ctx *context) *cobra.Command {
	c := &versionCommand{
		Version: ctx.Version,
		Stdout:  ctx.Stdout,
	}

	return &cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Run: func(cmd *cobra.Command, args []string) {
			c.execute()
		},
	}
}

// execute prints the version information.
func (c *versionCommand) execute() {
	_, _ = fmt.Fprintf(c.Stdout, "tell-me-go version %s\n", c.Version)
}
