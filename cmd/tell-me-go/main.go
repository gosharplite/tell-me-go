// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/cli"
	_ "github.com/gosharplite/tell-me-go/internal/cli/commands/chat"
	_ "github.com/gosharplite/tell-me-go/internal/cli/commands/version"
)

const Version = "2.5.0"

func main() {
	app := cli.New(Version, os.Stdin, os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
