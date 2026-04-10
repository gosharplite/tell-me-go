// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/gosharplite/tell-me-go/internal/cli"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/config"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/di"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/env"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/logging"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"
)

// Compile-time assertion to ensure DI Bootstrapper implements the CLI requirement.
var _ cli.Bootstrapper = (*di.Bootstrapper)(nil)

// version is the application version, usually set at build time via
// -ldflags="-X 'main.version=vX.Y.Z'".
var version = "dev"

func getVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
	}
	return version
}

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
		_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to create OTel resource: %v\n", err)
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpointURL(endpoint),
	)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: failed to create OTel exporter: %v\n", err)
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
	appVersion := getVersion()
	app, cleanup, err := buildApp(appVersion, os.Stdin, os.Stdout, os.Stderr)

	// Ensure cleanup runs if it was returned, regardless of error status
	if cleanup != nil {
		defer cleanup()
	}

	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error initializing application: %v\n", err)
		return 1
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

func buildApp(appVersion string, stdin io.Reader, stdout, stderr io.Writer) (*cli.App, func(), error) {
	ctx := context.Background()
	shutdown := initTracer(ctx)
	cleanup := func() {
		sCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(sCtx); err != nil {
			_, _ = fmt.Fprintf(stderr, "Error shutting down tracer: %v\n", err)
		}
	}

	// 1. Resolve basic environment
	homeDir := env.ResolveHomeDir(os.Getenv, os.UserHomeDir)

	// 2. Initialize Core Infrastructure
	sm := security.NewSecurityManager(nil)

	// 3. Setup Logger
	isDebug := os.Getenv("TELL_ME_DEBUG") == "1"
	logger := logging.NewLogger(stderr, isDebug)
	slog.SetDefault(logger)

	// 4. Build DI Container
	bootstrapper := di.NewBootstrapper(homeDir, sm, appVersion, stdout, stderr, logger, nil, nil)

	// 5. Instantiate ChatService
	chatService := bootstrapper.GetChatService()

	// 6. Initialize CLI with pre-wired dependencies
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	configLoader := &config.YAMLConfigLoader{
		Finder: config.NewDefaultConfigFinder(config.WithBaseDir(wd)),
	}
	app, err := cli.New(cli.AppDependencies{
		Version:      appVersion,
		Stdin:        stdin,
		Stdout:       stdout,
		Stderr:       stderr,
		HomeDir:      homeDir,
		SM:           sm,
		Bootstrapper: bootstrapper,
		ConfigLoader: configLoader,
		ChatService:  chatService,
	}, os.Getenv)

	if err != nil {
		return nil, cleanup, err
	}

	return app, cleanup, nil
}
