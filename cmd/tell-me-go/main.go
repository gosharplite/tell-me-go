// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/gosharplite/tell-me-go/internal/cli"
)

const version = "3.2.20"

func main() {
	app := cli.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
