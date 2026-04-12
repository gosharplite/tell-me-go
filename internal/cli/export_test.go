// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"github.com/spf13/cobra"
)

// Export internal symbols for external test package
type Context = context
type CliOptions = cliOptions

func NewChatCommand(ctx *context, opts *cliOptions) *cobra.Command {
	return newChatCommand(ctx, opts)
}
