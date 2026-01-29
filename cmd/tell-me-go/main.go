// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"github.com/gosharplite/tell-me-go/internal/cli"
)

const Version = "1.49.0"

func main() {
	app := cli.New(Version)
	app.Run()
}
