// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/cli/command"
	_ "github.com/gosharplite/tell-me-go/internal/cli/commands/version"
)

func TestNew(t *testing.T) {
	t.Run("Default home directory", func(t *testing.T) {
		os.Unsetenv("TELL_ME_HOME")
		os.Unsetenv("AIT_HOME")
		app := New("1.0.0", nil, nil, nil)
		if app.homeDir != "." {
			t.Errorf("expected homeDir to be '.', got %s", app.homeDir)
		}
	})

	t.Run("TELL_ME_HOME environment variable", func(t *testing.T) {
		expected := "/tmp/tell-me-home"
		os.Setenv("TELL_ME_HOME", expected)
		defer os.Unsetenv("TELL_ME_HOME")
		app := New("1.0.0", nil, nil, nil)
		if app.homeDir != expected {
			t.Errorf("expected homeDir to be %s, got %s", expected, app.homeDir)
		}
	})

	t.Run("AIT_HOME environment variable", func(t *testing.T) {
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
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	version := "1.2.3"
	app := New(version, nil, stdout, stderr)

	err := app.Run(context.Background(), []string{"--version"})
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
	app := New("1.0.0", nil, stdout, stderr)

	// "chat" is not registered in this test yet, so it should fail.
	err := app.Run(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error for unregistered 'chat' command, got nil")
	}
}

type mockCommand struct {
	err error
}

func (m *mockCommand) Execute(ctx context.Context, args []string) error {
	return m.err
}

func TestApp_Run_ContextCanceled(t *testing.T) {
	// Register a mock command for "chat" to test error handling
	command.Register("chat", func(ctx *command.Context) command.Command {
		return &mockCommand{err: context.Canceled}
	})

	stderr := &bytes.Buffer{}
	app := New("1.0.0", nil, nil, stderr)

	ctx, cancel := context.WithCancel(context.Background())
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
	command.Register("error-cmd", func(ctx *command.Context) command.Command {
		return &mockCommand{err: customErr}
	})

	// Since App.Run doesn't allow choosing a command name except via hardcoded logic,
	// we'll temporarily hijack "chat" again (it's already registered by previous test).
	command.Register("chat", func(ctx *command.Context) command.Command {
		return &mockCommand{err: customErr}
	})

	app := New("1.0.0", nil, nil, nil)
	err := app.Run(context.Background(), []string{})
	if !errors.Is(err, customErr) {
		t.Errorf("expected %v, got %v", customErr, err)
	}
}
