// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/security"

	"go.opentelemetry.io/otel/sdk/resource"
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

func TestBuildApp_CleanupError(t *testing.T) {
	// 1. Save and defer restoration of the tracer function
	origTracer := initTracerFn
	t.Cleanup(func() { initTracerFn = origTracer })

	// 2. Inject a tracer that returns a shutdown which always fails
	shutdownErr := errors.New("injected shutdown failure")
	initTracerFn = func(ctx context.Context) func(context.Context) error {
		return func(_ context.Context) error {
			return shutdownErr
		}
	}

	// 3. Capture stderr
	var stderrBuf strings.Builder

	// 4. Build the app (stderr is captured; env is isolated)
	t.Setenv("TELL_ME_HOME", t.TempDir())
	app, cleanup, err := buildApp("test-version", strings.NewReader(""), io.Discard, &stderrBuf)
	if err != nil {
		t.Fatalf("buildApp failed: %v", err)
	}
	if app == nil {
		t.Fatal("buildApp returned nil app")
	}
	if cleanup == nil {
		t.Fatal("buildApp returned nil cleanup function")
	}

	// 5. Invoke cleanup — this should trigger the error path
	cleanup()

	// 6. Assert the stderr output contains the expected error message
	stderrOutput := stderrBuf.String()
	if !strings.Contains(stderrOutput, "Error shutting down tracer") {
		t.Errorf("expected stderr to contain 'Error shutting down tracer', got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, shutdownErr.Error()) {
		t.Errorf("expected stderr to contain %q, got: %q", shutdownErr.Error(), stderrOutput)
	}
}

func TestRun_BuildError(t *testing.T) {
	// 1. Save and defer restoration of buildApp function
	origBuildApp := buildAppFn
	t.Cleanup(func() { buildAppFn = origBuildApp })

	// 2. Save and defer restoration of os.Args
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })

	// 3. Inject a buildApp that returns an error
	buildErr := errors.New("injected build failure")
	buildAppFn = func(_ string, _ io.Reader, _, _ io.Writer) (*cli.App, func(), error) {
		return nil, nil, buildErr
	}

	// 4. Set safe args
	os.Args = []string{"tell-me-go", "-v"}

	// 5. Redirect stderr for capture
	oldStderr := os.Stderr
	t.Cleanup(func() { os.Stderr = oldStderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	// 6. Call run()
	exitCode := run()

	// 7. Close writer and read captured stderr
	if err := w.Close(); err != nil {
		t.Logf("closing stderr pipe writer: %v", err)
	}
	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, r)
	stderrOutput := stderrBuf.String()

	// 8. Assert exit code
	if exitCode != 1 {
		t.Errorf("expected exit code 1, got %d", exitCode)
	}

	// 9. Assert error message on stderr
	if !strings.Contains(stderrOutput, "Error initializing application") {
		t.Errorf("expected stderr to contain 'Error initializing application', got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, buildErr.Error()) {
		t.Errorf("expected stderr to contain %q, got: %q", buildErr.Error(), stderrOutput)
	}
}

func TestBuildApp_GetwdError(t *testing.T) {
	// This test is only safe on Linux.
	// On other platforms, os.Chdir into a deleted directory has
	// undefined behavior and may panic.
	if runtime.GOOS != "linux" {
		t.Skip("skipping os.Getwd failure test on non-Linux platform")
	}

	// 1. Save current working directory
	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get current working directory: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Logf("failed to restore working directory: %v", err)
		}
	})

	// 2. Create a temp directory and chdir into it
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("failed to chdir into temp dir: %v", err)
	}

	// 3. Remove the directory we're currently in.
	// On Linux, the kernel keeps the directory alive until we chdir out.
	if err := os.RemoveAll(dir); err != nil {
		t.Fatalf("failed to remove temp dir: %v", err)
	}

	// 4. Now os.Getwd() should fail.
	// Verify the precondition:
	if _, err := os.Getwd(); err == nil {
		t.Skip("os.Getwd() succeeded after removing CWD; kernel may have cached the path")
	}

	// 5. buildApp must handle the Getwd error gracefully
	t.Setenv("TELL_ME_HOME", oldWd)
	app, cleanup, err := buildApp("test-version", strings.NewReader(""), io.Discard, io.Discard)
	if err != nil {
		t.Fatalf("buildApp failed: %v", err)
	}
	if app == nil {
		t.Fatal("buildApp returned nil app despite Getwd error")
	}
	if cleanup == nil {
		t.Fatal("buildApp returned nil cleanup function")
	}
	cleanup()
}

func TestBuildApp_CLINewError(t *testing.T) {
	// 1. Save and defer restoration of overrides
	origCLINew := cliNewFn
	origNewSM := newSecurityManagerFn
	t.Cleanup(func() {
		cliNewFn = origCLINew
		newSecurityManagerFn = origNewSM
	})

	// 2. Capture the interactorProvider closure when newSecurityManagerFn is called
	var capturedProvider func() domain_security.UserInteractor
	newSecurityManagerFn = func(ip func() domain_security.UserInteractor) *security.SecurityManager {
		capturedProvider = ip
		return security.NewSecurityManager(ip)
	}

	// 3. Inject a cliNewFn that returns an error
	cliNewErr := errors.New("injected cli.New failure")
	cliNewFn = func(deps cli.AppDependencies, getenv func(string) string) (*cli.App, error) {
		return nil, cliNewErr
	}

	// 4. Isolate environment
	t.Setenv("TELL_ME_HOME", t.TempDir())

	// 5. Call buildApp directly — cliNewFn should fail
	app, cleanup, err := buildApp("test-version", strings.NewReader(""), io.Discard, io.Discard)

	// 6. Assert: error must be non-nil (cli.New failure)
	if err == nil {
		t.Fatal("expected error from buildApp due to cli.New failure, got nil")
	}
	if !errors.Is(err, cliNewErr) {
		t.Errorf("expected error %v, got %v", cliNewErr, err)
	}

	// 7. Assert: cleanup must be non-nil even on error (contract)
	if cleanup == nil {
		t.Fatal("expected non-nil cleanup function even on error")
	}

	// 8. Assert: app must be nil on error
	if app != nil {
		t.Error("expected nil app on error")
	}

	// 9. Exercise the interactorProvider closure to cover its body
	if capturedProvider == nil {
		t.Fatal("expected captured interactorProvider, got nil")
	}
	interactor := capturedProvider()
	if interactor == nil {
		t.Fatal("expected non-nil interactor from provider")
	}

	// 10. Run cleanup (must not panic)
	cleanup()
}

func TestInitTracer_ResourceError(t *testing.T) {
	// 1. Save and defer restoration of the resource constructor
	origResource := newResourceFn
	t.Cleanup(func() { newResourceFn = origResource })

	// 2. Inject a failing resource.New
	resourceErr := errors.New("injected resource failure")
	newResourceFn = func(_ context.Context, _ ...resource.Option) (*resource.Resource, error) {
		return nil, resourceErr
	}

	// 3. Set a valid endpoint so we enter the resource creation path
	t.Setenv("TELL_ME_TRACE_ENDPOINT", "http://localhost:4318")

	// 4. Capture stderr
	oldStderr := os.Stderr
	t.Cleanup(func() { os.Stderr = oldStderr })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w

	// 5. Call the REAL initTracer (not via initTracerFn) so the injected
	//    newResourceFn is exercised through the actual error path.
	shutdown := initTracer(context.Background())

	// 6. Close writer and read captured stderr
	if err := w.Close(); err != nil {
		t.Logf("closing stderr pipe writer: %v", err)
	}
	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, r)
	stderrOutput := stderrBuf.String()

	// 7. initTracer must return a non-nil shutdown even on error
	if shutdown == nil {
		t.Fatal("initTracer returned nil shutdown on resource error")
	}

	// 8. The fallback shutdown must not error (noop tracer)
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("expected nil error from fallback shutdown, got %v", err)
	}

	// 9. Assert warning message on stderr
	if !strings.Contains(stderrOutput, "Warning: failed to create OTel resource") {
		t.Errorf("expected stderr to contain 'Warning: failed to create OTel resource', got: %q", stderrOutput)
	}
	if !strings.Contains(stderrOutput, resourceErr.Error()) {
		t.Errorf("expected stderr to contain %q, got: %q", resourceErr.Error(), stderrOutput)
	}
}
