// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMain_Execution(t *testing.T) {
	// If we are in the subprocess, run main() and exit

	if os.Getenv("TEST_MAIN_EXEC") == "1" {
		// Override args to something safe like -v
		os.Args = []string{"tell-me-go", "-v"}
		main()
		return
	}

	// Otherwise, spawn the subprocess to test main() safely
	cmd := exec.Command(os.Args[0], "-test.run=TestMain_Execution")
	cmd.Env = append(os.Environ(), "TEST_MAIN_EXEC=1")
	err := cmd.Run()

	// We expect a clean exit (0) because we passed "-v"
	if err != nil {
		t.Fatalf("process ran with err %v, want exit status 0", err)
	}
}

func TestMainBootstrap(t *testing.T) {
	// Simple smoke test to ensure version is defined

	if version == "" {
		t.Error("version should not be empty")
	}
}

func TestInitTracer_NoEndpoint(t *testing.T) {
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "")
	shutdown := initTracer(context.Background())
	if shutdown == nil {
		t.Fatal("initTracer returned nil shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown failed: %v", err)
	}
}

func TestInitTracer_WithEndpoint(t *testing.T) {
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "http://localhost:4318")
	shutdown := initTracer(context.Background())
	if shutdown == nil {
		t.Fatal("initTracer returned nil shutdown function")
	}
	_ = shutdown(context.Background())
}

func TestInitTracer_InvalidEndpoint(t *testing.T) {
	// A malformed URL with a space should trigger an error in otlptracehttp.New
	t.Setenv("TELL_ME_TRACE_ENDPOINT", " http://localhost")
	shutdown := initTracer(context.Background())
	if shutdown == nil {
		t.Fatal("initTracer returned nil shutdown function")
	}
	_ = shutdown(context.Background())
}

func TestInitTracer_ExporterError(t *testing.T) {
	// Provide a malformed URL to force otlptracehttp.New to return an error
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "://invalid-url")

	shutdown := initTracer(context.Background())

	if shutdown == nil {
		t.Fatal("expected fallback shutdown function, got nil")
	}

	// Ensure the fallback shutdown doesn't panic
	err := shutdown(context.Background())
	if err != nil {
		t.Errorf("expected nil error from fallback shutdown, got %v", err)
	}
}

func TestRun_InvalidArgs(t *testing.T) {
	// Save original args

	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set invalid args to trigger a failure in cli.App.Run
	os.Args = []string{"tell-me-go", "--invalid-flag-that-does-not-exist"}

	exitCode := run()
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}
}

func TestRun_Success(t *testing.T) {
	// Save original args
	oldArgs := os.Args
	defer func() { os.Args = oldArgs }()

	// Set valid args that just show version and exit
	os.Args = []string{"tell-me-go", "-v"}
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "")

	exitCode := run()
	if exitCode != 0 {
		t.Errorf("expected exit code 0, got %d", exitCode)
	}
}

func TestInitTracer_ContextCanceled(t *testing.T) {
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "http://localhost:4318")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	shutdown := initTracer(ctx)
	if shutdown == nil {
		t.Fatal("initTracer returned nil shutdown function")
	}
	_ = shutdown(context.Background())
}

func TestGetVersion(t *testing.T) {
	// 1. Save global state and defer restoration
	originalVersion := version
	t.Cleanup(func() {
		version = originalVersion
	})

	// 2. Test explicit ldflags override
	t.Run("returns explicitly set version", func(t *testing.T) {
		version = "v1.2.3"
		got := getVersion()
		if got != "v1.2.3" {
			t.Errorf("getVersion() = %v, want v1.2.3", got)
		}
	})

	// 3. Test default/fallback behavior
	t.Run("returns dev or devel when info is unavailable", func(t *testing.T) {
		version = "dev"
		got := getVersion()
		if got != "dev" && got != "(devel)" {
			t.Errorf("getVersion() = %v, want dev or (devel)", got)
		}
	})
}

func TestBuildApp_Smoke(t *testing.T) {
	// Isolate environment to prevent host machine pollution
	t.Setenv("TELL_ME_HOME", t.TempDir())

	app, cleanup, err := buildApp("test-version", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("buildApp failed: %v", err)
	}
	if app == nil {
		t.Fatal("buildApp returned nil app")
	}
	if cleanup == nil {
		t.Fatal("buildApp returned nil cleanup function")
	}
	defer cleanup()
}
