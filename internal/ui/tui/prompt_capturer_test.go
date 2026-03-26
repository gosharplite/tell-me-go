// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"context"
	"flag"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockBaseCapturer struct {
	promptMsgs []string
}

func (m *mockBaseCapturer) IsTTY(v any) bool { return false }
func (m *mockBaseCapturer) CapturePrompt(ctx context.Context, fs *flag.FlagSet, opts ...ports.CaptureOption) (string, error) {
	return "base prompt", nil
}
func (m *mockBaseCapturer) Confirm(ctx context.Context, message string) (bool, error) { return true, nil }
func (m *mockBaseCapturer) Warn(message string)                                      {}
func (m *mockBaseCapturer) Prompt(message string) {
	m.promptMsgs = append(m.promptMsgs, message)
}
func (m *mockBaseCapturer) ReadLine(ctx context.Context) (string, error)     { return "", nil }
func (m *mockBaseCapturer) ReadSingleKey(ctx context.Context) (string, error) { return "", nil }

type mockSuggestionService struct {
	recordedPrompts []string
}

func (m *mockSuggestionService) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (m *mockSuggestionService) RecordPrompt(prompt string) error {
	m.recordedPrompts = append(m.recordedPrompts, prompt)
	return nil
}

func TestPromptCapturer_CapturePrompt_Fallback(t *testing.T) {
	base := &mockBaseCapturer{}
	svc := &mockSuggestionService{}
	capturer := NewPromptCapturer(base, svc, "provider", "model")

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt, err := capturer.CapturePrompt(context.Background(), fs) // No UseTUIPrompt option

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompt != "base prompt" {
		t.Errorf("expected 'base prompt', got %q", prompt)
	}
}

type mockBaseCapturerWithTTY struct {
	mockBaseCapturer
	isTTY bool
}

func (m *mockBaseCapturerWithTTY) IsTTY(v any) bool { return m.isTTY }

func TestPromptCapturer_CapturePrompt_Fallback_Conditions(t *testing.T) {
	base := &mockBaseCapturerWithTTY{isTTY: true}
	svc := &mockSuggestionService{}
	capturer := NewPromptCapturer(base, svc, "provider", "model")

	ctx := context.Background()

	t.Run("fallback when SkipTTYWait is true", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		prompt, err := capturer.CapturePrompt(ctx, fs, ports.WithTUIPrompt(true), ports.WithSkipTTYWait(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prompt != "base prompt" {
			t.Errorf("expected 'base prompt', got %q", prompt)
		}
	})

	t.Run("fallback when positional arguments are present", func(t *testing.T) {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		_ = fs.Parse([]string{"hello"}) // Set one positional argument
		prompt, err := capturer.CapturePrompt(ctx, fs, ports.WithTUIPrompt(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prompt != "base prompt" {
			t.Errorf("expected 'base prompt', got %q", prompt)
		}
	})
}

func TestPromptCapturer_IsTTY(t *testing.T) {
	base := &mockBaseCapturer{}
	capturer := NewPromptCapturer(base, nil, "", "")
	if capturer.IsTTY(nil) != false {
		t.Error("expected IsTTY to delegate to base")
	}
}

func TestPromptCapturer_UserInteractorDelegation(t *testing.T) {
	base := &mockBaseCapturer{}
	capturer := NewPromptCapturer(base, nil, "", "")

	ctx := context.Background()
	_, _ = capturer.Confirm(ctx, "test")
	capturer.Warn("test")
	capturer.Prompt("test")
	_, _ = capturer.ReadLine(ctx)
	_, _ = capturer.ReadSingleKey(ctx)

	if len(base.promptMsgs) != 1 || base.promptMsgs[0] != "test" {
		t.Errorf("expected Prompt to delegate to base, got %v", base.promptMsgs)
	}
}
