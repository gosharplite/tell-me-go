// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/cli"
)

const Version = "1.68.0-dev"

func main() {
	app := cli.New(Version)
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
