// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package tui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

type mockBaseCapturer struct {
	promptMsgs []string
}

func (m *mockBaseCapturer) IsTTY(v any) bool { return false }
func (m *mockBaseCapturer) CapturePrompt(ctx context.Context, args []string, opts ...ports.CaptureOption) (string, error) {
	return "base prompt", nil
}
func (m *mockBaseCapturer) Confirm(ctx context.Context, message string) (bool, error) {
	return true, nil
}
func (m *mockBaseCapturer) Warn(message string) {}
func (m *mockBaseCapturer) Prompt(message string) {
	m.promptMsgs = append(m.promptMsgs, message)
}
func (m *mockBaseCapturer) ReadLine(ctx context.Context) (string, error)      { return "", nil }
func (m *mockBaseCapturer) ReadSingleKey(ctx context.Context) (string, error) { return "", nil }
func (m *mockBaseCapturer) Close(ctx context.Context) error                   { return nil }

type mockSuggestionService struct {
	recordedPrompts []string
}

func (m *mockSuggestionService) GetSuggestions(ctx context.Context, prefix string) ([]string, error) {
	return nil, nil
}
func (m *mockSuggestionService) RecordPrompt(ctx context.Context, prompt string) error {
	m.recordedPrompts = append(m.recordedPrompts, prompt)
	return nil
}
func (m *mockSuggestionService) Close(ctx context.Context) error {
	return nil
}

func TestPromptCapturer_CapturePrompt_Fallback(t *testing.T) {
	base := &mockBaseCapturer{}
	svc := &mockSuggestionService{}
	capturer := NewPromptCapturer(base, svc)

	prompt, err := capturer.CapturePrompt(context.Background(), nil) // No UseTUIPrompt option

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
	capturer := NewPromptCapturer(base, svc)

	ctx := context.Background()

	t.Run("fallback when SkipTTYWait is true", func(t *testing.T) {
		prompt, err := capturer.CapturePrompt(ctx, nil, ports.WithTUIPrompt(true), ports.WithSkipTTYWait(true))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prompt != "base prompt" {
			t.Errorf("expected 'base prompt', got %q", prompt)
		}
	})

	t.Run("fallback when positional arguments are present", func(t *testing.T) {
		prompt, err := capturer.CapturePrompt(ctx, []string{"hello"}, ports.WithTUIPrompt(true))
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
	capturer := NewPromptCapturer(base, nil)
	if capturer.IsTTY(nil) != false {
		t.Error("expected IsTTY to be false for nil")
	}
}

func TestPromptCapturer_UserInteractorDelegation(t *testing.T) {
	base := &mockBaseCapturer{}
	capturer := NewPromptCapturer(base, nil)

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

func TestPromptCapturer_CapturePrompt_TUI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "successful input",
			input:    "mocked command\x13", // simulate typing and Ctrl+S to submit
			expected: "mocked command",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := &mockBaseCapturerWithTTY{isTTY: true}
			svc := &mockSuggestionService{}

			var in bytes.Buffer
			in.WriteString(tt.input)

			capturer := NewPromptCapturer(
				base,
				svc,
				withProgramOptions(
					tea.WithInput(&in),
					tea.WithOutput(io.Discard),
				),
			)

			got, err := capturer.CapturePrompt(context.Background(), nil, ports.WithTUIPrompt(true))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.expected {
				t.Errorf("got %q; want %q", got, tt.expected)
			}

			// Verify it was recorded in the suggestion service
			found := false
			for _, p := range svc.recordedPrompts {
				if p == tt.expected {
					found = true
					break
				}
			}
			if !found {
				t.Error("expected prompt to be recorded in suggestion service")
			}
		})
	}
}

func TestPromptCapturer_CapturePrompt_TUI_Empty(t *testing.T) {
	base := &mockBaseCapturerWithTTY{isTTY: true}
	svc := &mockSuggestionService{}

	var in bytes.Buffer
	in.WriteString("\x03") // Send Ctrl+C to abort/exit

	capturer := NewPromptCapturer(
		base,
		svc,
		withProgramOptions(
			tea.WithInput(&in),
			tea.WithOutput(io.Discard),
		),
	)

	_, err := capturer.CapturePrompt(context.Background(), nil, ports.WithTUIPrompt(true))
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled for empty TUI prompt, got %v", err)
	}
}
