// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"errors"
	"testing"
)

func TestNew(t *testing.T) {
	t.Run("Default home directory", func(t *testing.T) {
		t.Setenv("TELL_ME_HOME", "")
		t.Setenv("AIT_HOME", "")
		app, _ := New("1.0.0", nil, nil, nil)
		if app.homeDir != "." {
			t.Errorf("expected homeDir to be '.', got %s", app.homeDir)
		}
	})

	t.Run("TELL_ME_HOME environment variable", func(t *testing.T) {
		expected := "/tmp/tell-me-home"
		t.Setenv("TELL_ME_HOME", expected)
		app, _ := New("1.0.0", nil, nil, nil)
		if app.homeDir != expected {
			t.Errorf("expected homeDir to be %s, got %s", expected, app.homeDir)
		}
	})

	t.Run("AIT_HOME environment variable", func(t *testing.T) {
		t.Setenv("TELL_ME_HOME", "")
		expected := "/tmp/ait-home"
		t.Setenv("AIT_HOME", expected)
		app, _ := New("1.0.0", nil, nil, nil)
		if app.homeDir != expected {
			t.Errorf("expected homeDir to be %s, got %s", expected, app.homeDir)
		}
	})
}

func TestApp_Run_Version(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	version := "1.2.3"
	app, _ := New(version, nil, stdout, stderr)

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
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	app, _ := New("1.0.0", nil, stdout, stderr)

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
	// Register a mock command for "chat" to test error handling

	register("chat", func(ctx *context) command {
		return &mockCommand{err: stdctx.Canceled}
	})

	stderr := &bytes.Buffer{}
	app, _ := New("1.0.0", nil, nil, stderr)

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

	app, _ := New("1.0.0", nil, nil, nil)
	err := app.Run(stdctx.Background(), []string{})
	if !errors.Is(err, customErr) {
		t.Errorf("expected %v, got %v", customErr, err)
	}
}

func TestNew_LogLevel(t *testing.T) {
	t.Run("Default to LevelWarn", func(t *testing.T) {
		t.Setenv("TELL_ME_DEBUG", "")
		stderr := &bytes.Buffer{}
		_, logger := New("1.0.0", nil, nil, stderr)

		logger.Info("this info message should NOT appear")
		logger.Warn("this warn message should appear")

		output := stderr.String()
		if bytes.Contains([]byte(output), []byte("level=INFO")) {
			t.Errorf("expected no INFO log, got %q", output)
		}
		if !bytes.Contains([]byte(output), []byte("level=WARN")) {
			t.Errorf("expected WARN log, got %q", output)
		}
	})

	t.Run("TELL_ME_DEBUG=1 enables LevelDebug", func(t *testing.T) {
		t.Setenv("TELL_ME_DEBUG", "1")
		stderr := &bytes.Buffer{}
		_, logger := New("1.0.0", nil, nil, stderr)

		logger.Debug("this debug message should appear")
		logger.Info("this info message should appear")

		output := stderr.String()
		if !bytes.Contains([]byte(output), []byte("level=DEBUG")) {
			t.Errorf("expected DEBUG log, got %q", output)
		}
		if !bytes.Contains([]byte(output), []byte("level=INFO")) {
			t.Errorf("expected INFO log, got %q", output)
		}
	})
}
