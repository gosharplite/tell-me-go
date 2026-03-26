// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
)

type mockChatService struct {
	chatCalled bool
	lastParams agent.ChatOptions
}

func (m *mockChatService) ProcessMessage(ctx stdctx.Context, opts agent.ChatOptions, capturer ports.Capturer) error {
	m.chatCalled = true
	m.lastParams = opts
	return nil
}

func (m *mockChatService) GetLastUserMessage(ctx stdctx.Context, configPath string) (string, int, error) {
	return "retry test", 1, nil
}

func (m *mockChatService) BrowseHistory(ctx stdctx.Context, configPath string, capturer ports.Capturer) error {
	return nil
}

func (m *mockChatService) GetToolNames(ctx stdctx.Context, configPath string) ([]string, error) {
	return []string{"test_tool"}, nil
}

type mockSM struct {
	domain_security.Manager
}

func (m *mockSM) SetInteractor(interactor domain_security.UserInteractor) {}
func (m *mockSM) TerminalLock()                                           {}
func (m *mockSM) TerminalUnlock()                                         {}
func (m *mockSM) Close() error                                            { return nil }

func TestChatCommand_Execute(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
		MockPrompt:  "hello",
	}

	ctx := stdctx.Background()
	args := []string{"chat", "hello"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.Prompt != "hello" {
		t.Errorf("expected prompt 'hello', got %q", mService.lastParams.Prompt)
	}
}

func TestChatCommand_Execute_LastN(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-l", "5"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.LastN != 5 {
		t.Errorf("expected LastN 5, got %d", mService.lastParams.LastN)
	}
}

func TestChatCommand_Execute_BackN(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader(""),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-b", "2"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.BackN != 2 {
		t.Errorf("expected BackN 2, got %d", mService.lastParams.BackN)
	}
}

func TestChatCommand_Execute_Retry(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader("y\n"),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-retry"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if !mService.chatCalled {
		t.Error("expected chat service to be called")
	}

	if mService.lastParams.Prompt != "retry test" {
		t.Errorf("expected prompt 'retry test', got %q", mService.lastParams.Prompt)
	}

	if mService.lastParams.BackN != 1 {
		t.Errorf("expected BackN 1, got %d", mService.lastParams.BackN)
	}

	if !strings.Contains(stdout.String(), "Are you sure you want to retry") {
		t.Errorf("expected stdout to contain retry message, got %q", stdout.String())
	}
}

func TestChatCommand_Execute_Retry_Aborted(t *testing.T) {
	t.Parallel()

	var stdout, stderr strings.Builder
	sm := &mockSM{}
	mService := &mockChatService{}

	cmd := &chatCommand{
		Version:     "1.0.0",
		Stdin:       strings.NewReader("n\n"),
		Stdout:      &stdout,
		Stderr:      &stderr,
		SM:          sm,
		ChatService: mService,
	}

	ctx := stdctx.Background()
	args := []string{"chat", "-retry"}

	err := cmd.Execute(ctx, args)
	if err != nil {
		t.Errorf("Execute failed: %v", err)
	}

	if mService.chatCalled {
		t.Error("expected chat service NOT to be called")
	}

	if !strings.Contains(stdout.String(), "Are you sure you want to retry") {
		t.Errorf("expected stdout to contain retry message, got %q", stdout.String())
	}
}
