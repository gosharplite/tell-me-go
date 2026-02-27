// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"errors"
	"os"
	"testing"
)

func TestNew(t *testing.T) {
	t.Parallel()
	t.Run("Default home directory", func(t *testing.T) {
		t.Parallel()
		os.Unsetenv("TELL_ME_HOME")
		os.Unsetenv("AIT_HOME")
		app := New("1.0.0", nil, nil, nil)
		if app.homeDir != "." {
			t.Errorf("expected homeDir to be '.', got %s", app.homeDir)
		}
	})

	t.Run("TELL_ME_HOME environment variable", func(t *testing.T) {
		t.Parallel()
		expected := "/tmp/tell-me-home"
		os.Setenv("TELL_ME_HOME", expected)
		defer os.Unsetenv("TELL_ME_HOME")
		app := New("1.0.0", nil, nil, nil)
		if app.homeDir != expected {
			t.Errorf("expected homeDir to be %s, got %s", expected, app.homeDir)
		}
	})

	t.Run("AIT_HOME environment variable", func(t *testing.T) {
		t.Parallel()
		os.Unsetenv("TELL_ME_HOME")
		expected := "/tmp/ait-home"
		os.Setenv("AIT_HOME", expected)
		defer os.Unsetenv("AIT_HOME")
		app := New("1.0.0", nil, nil, nil)
		if app.homeDir != expected {
			t.Errorf("expected homeDir to be %s, got %s", expected, app.homeDir)
		}
	})
}

func TestApp_Run_Version(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	version := "1.2.3"
	app := New(version, nil, stdout, stderr)

	err := app.Run(stdctx.Background(), []string{"--version"})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	output := stdout.String()
	expected := "tell-me-go version " + version + "\n"
	if output != expected {
		t.Errorf("expected output %q, got %q", expected, output)
	}
}

func TestApp_Run_UnknownCommand(t *testing.T) {
	t.Parallel()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app := New("1.0.0", nil, stdout, stderr)

	// "chat" is not registered in this test yet, so it should fail.
	err := app.Run(stdctx.Background(), []string{})
	if err == nil {
		t.Fatal("expected error for unregistered 'chat' command, got nil")
	}
}

type mockCommand struct {
	err error
}

func (m *mockCommand) Execute(ctx stdctx.Context, args []string) error {
	return m.err
}

func TestApp_Run_ContextCanceled(t *testing.T) {
	t.Parallel()
	// Register a mock command for "chat" to test error handling

	register("chat", func(ctx *context) command {
		return &mockCommand{err: stdctx.Canceled}
	})

	stderr := &bytes.Buffer{}
	app := New("1.0.0", nil, nil, stderr)

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	cancel() // Cancel it immediately

	err := app.Run(ctx, []string{})
	if err != nil {
		t.Errorf("expected nil error on context cancellation, got %v", err)
	}

	// App.Run prints a newline to stderr on context cancellation
	if stderr.String() != "\n" {
		t.Errorf("expected newline on stderr, got %q", stderr.String())
	}
}

func TestApp_Run_CommandError(t *testing.T) {
	t.Parallel()
	// Register a mock command for a custom error

	customErr := errors.New("custom error")
	register("error-cmd", func(ctx *context) command {
		return &mockCommand{err: customErr}
	})

	// Since App.Run doesn't allow choosing a command name except via hardcoded logic,
	// we'll temporarily hijack "chat" again (it's already registered by previous test).
	register("chat", func(ctx *context) command {
		return &mockCommand{err: customErr}
	})

	app := New("1.0.0", nil, nil, nil)
	err := app.Run(stdctx.Background(), []string{})
	if !errors.Is(err, customErr) {
		t.Errorf("expected %v, got %v", customErr, err)
	}
}
