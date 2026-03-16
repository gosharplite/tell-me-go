// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"os/exec"
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
	// Simple smoke test to ensure Version is defined

	if Version == "" {
		t.Error("Version should not be empty")
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
