// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/gosharplite/tell-me-go/internal/cli"
)

const version = "5.4.0-dev"

func initTracer(ctx context.Context) func(context.Context) error {
	endpoint := os.Getenv("TELL_ME_TRACE_ENDPOINT")
	if endpoint == "" {
		// Use a global no-op tracer provider so spans are silent by default
		// but the application logic remains fully instrumented.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("tell-me-go"),
		),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create OTel resource: %v\n", err)
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to create OTel exporter: %v\n", err)
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown
}

func main() {
	os.Exit(run())
}

func run() int {
	ctx := context.Background()
	shutdown := initTracer(ctx)
	defer func() {
		sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(sCtx); err != nil {
			fmt.Fprintf(os.Stderr, "Error shutting down tracer: %v\n", err)
		}
	}()

	app := cli.New(version, os.Stdin, os.Stdout, os.Stderr)
	if err := app.Run(ctx, os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}
