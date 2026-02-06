// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/cli"
	_ "github.com/gosharplite/tell-me-go/internal/cli/commands/chat"
	_ "github.com/gosharplite/tell-me-go/internal/cli/commands/version"
)

const Version = "1.91.0"

func main() {
	app := cli.New(Version)
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
