// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package cli

import (
	stdctx "context"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	domain_security "github.com/gosharplite/tell-me-go/internal/domain/security"
	orchestration_app "github.com/gosharplite/tell-me-go/internal/orchestration"
)

type mockChatService struct {
	chatCalled bool
	lastParams orchestration_app.ChatOptions
}

func (m *mockChatService) ProcessMessage(ctx stdctx.Context, opts orchestration_app.ChatOptions, capturer orchestration.Capturer) error {
	m.chatCalled = true
	m.lastParams = opts
	return nil
}

type mockSM struct {
	domain_security.ISecurityManager
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
