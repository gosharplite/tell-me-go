// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/gosharplite/tell-me-go/internal/cli"
)

const Version = "1.20.0-dev"

func main() {
	app := cli.New(Version)
	app.Run()
}
