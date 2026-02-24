// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/gosharplite/tell-me-go/internal/cli"
)

const version = "3.2.45"

func initTracer() {
	// Use a global no-op tracer provider so spans are silent by default
	// but the application logic remains fully instrumented.
	otel.SetTracerProvider(noop.NewTracerProvider())
}

func main() {
	initTracer()

	app := cli.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
