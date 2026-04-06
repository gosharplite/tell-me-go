// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	"bytes"
	stdctx "context"
	"errors"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/config"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestApp_Run_Version(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	version := "1.2.3"
	app := New(version, nil, stdout, stderr, &simpleMockBootstrapper{}, nil, ".", nil, &cliMockLoader{})

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
	app := New("1.0.0", nil, stdout, stderr, &simpleMockBootstrapper{}, nil, ".", nil, &cliMockLoader{})

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
	app := New("1.0.0", nil, nil, stderr, &simpleMockBootstrapper{}, nil, ".", nil, &cliMockLoader{})

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

	app := New("1.0.0", nil, nil, nil, &simpleMockBootstrapper{}, nil, ".", nil, &cliMockLoader{})
	err := app.Run(stdctx.Background(), []string{})
	if !errors.Is(err, customErr) {
		t.Errorf("expected %v, got %v", customErr, err)
	}
}

func TestApp_Run_NilBootstrapper(t *testing.T) {
	// Pass nil for the bootstrapper (5th argument)
	app := New("1.0.0", nil, nil, nil, nil, nil, ".", nil, &cliMockLoader{})

	// Execute a command that would normally trigger the bootstrapper
	err := app.Run(stdctx.Background(), []string{"chat"})

	expectedErr := "application bootstrapper is not initialized"
	if err == nil || err.Error() != expectedErr {
		t.Errorf("expected error %q, got %v", expectedErr, err)
	}
}

type cliMockLoader struct{}

func (m *cliMockLoader) Load(path string) (*config.Config, error) {
	return nil, errors.New("not implemented")
}

type simpleMockBootstrapper struct {
	Bootstrapper // Embed the interface to automatically satisfy unused methods
}

// Retain ONLY the methods that are strictly invoked during app.Run() setup
func (m *simpleMockBootstrapper) GetAgentFactory() ports.ChatterFactory     { return nil }
func (m *simpleMockBootstrapper) GetUIRenderer() ports.UIRenderer           { return nil }
func (m *simpleMockBootstrapper) GetHistoryRenderer() ports.HistoryRenderer { return nil }
func (m *simpleMockBootstrapper) GetHistoryBrowser() ports.HistoryBrowser   { return nil }
