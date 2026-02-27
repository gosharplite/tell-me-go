// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"flag"
	"io"
	"os"
	"strings"
	"testing"
)

func TestCapturePromptContextCancellation(t *testing.T) {
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.CapturePrompt(ctx, fs, 0, false)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPrompt_Pipe(t *testing.T) {
	inputStr := "hello from pipe"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	prompt, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != inputStr {
		t.Errorf("expected %q, got %q", inputStr, prompt)
	}
}

func TestPrompt_Args(t *testing.T) {
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", prompt)
	}
}

func TestPrompt_Empty(t *testing.T) {
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestPrompt_MockEnv(t *testing.T) {
	os.Setenv("TELL_ME_MOCK_PROMPT", "mocked prompt")
	defer os.Unsetenv("TELL_ME_MOCK_PROMPT")

	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "mocked prompt" {
		t.Errorf("expected 'mocked prompt', got %q", prompt)
	}
}

func TestPrompt_EmptyPipe(t *testing.T) {
	// Empty stdin (simulated pipe)

	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial", "prompt"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the initial prompt from args
	if prompt != "initial prompt" {
		t.Errorf("expected 'initial prompt', got %q", prompt)
	}
}

func TestPrintFeedback_NoSM(t *testing.T) {
	var buf bytes.Buffer
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: &buf,
		Stderr: io.Discard,
		SM:     nil, // No security manager
	}

	// Should not panic and should print the message
	capturer.printFeedback(&buf, false, "", "test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected output to contain 'test message', got %q", buf.String())
	}
}

func TestPrompt_Combined(t *testing.T) {
	inputStr := "pipe input"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "initial\npipe input"
	if prompt != expected {
		t.Errorf("expected %q, got %q", expected, prompt)
	}
}

func TestIsTTY_False(t *testing.T) {
	capturer := &capturer{}
	if capturer.IsTTY("not a file") {
		t.Error("expected IsTTY to be false for string")
	}
}

func TestCaptureFromTTY(t *testing.T) {
	inputStr := "tty input"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	prompt, err := capturer.captureFromTTY(context.Background(), false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != inputStr {
		t.Errorf("expected %q, got %q", inputStr, prompt)
	}
}

func TestCaptureFromTTY_Cancel(t *testing.T) {
	capturer := &capturer{
		Stdin:  strings.NewReader("never read"),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := capturer.captureFromTTY(ctx, false)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestWarn_SemanticStyling(t *testing.T) {
	var stderr bytes.Buffer
	isTTY := true
	capturer := &capturer{
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
	}

	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "Security warning",
			message:  "[SECURITY] High risk",
			expected: colorRed + "[SECURITY] High risk" + colorReset + "\n",
		},
		{
			name:     "Normal warning",
			message:  "Regular warning",
			expected: colorYellow + "Regular warning" + colorReset + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
					stderr.Reset()
			capturer.Warn(tt.message)
			if stderr.String() != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, stderr.String())
			}
		})
	}
}

func TestConfirm_SemanticStyling(t *testing.T) {
	os.Setenv("TELL_ME_MOCK_ANSWER", "y")
	defer os.Unsetenv("TELL_ME_MOCK_ANSWER")

	var stderr bytes.Buffer
	isTTY := true
	capturer := &capturer{
		Stdin:         strings.NewReader(""),
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
	}

	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "Security confirmation",
			message:  "[SECURITY] Proceed?",
			expected: colorRed + "[SECURITY] Proceed?" + colorReset,
		},
		{
			name:     "Required confirmation",
			message:  "[CONFIRMATION REQUIRED] Are you sure?",
			expected: colorRed + "[CONFIRMATION REQUIRED] Are you sure?" + colorReset,
		},
		{
			name:     "Normal confirmation",
			message:  "Continue?",
			expected: "Continue?", // Normal confirmation doesn't have a special color logic in the provided implementation snippet other than writing the message directly
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
					stderr.Reset()
			_, err := capturer.Confirm(context.Background(), tt.message)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stderr.String() != tt.expected+"\n" {
				t.Errorf("expected %q, got %q", tt.expected+"\n", stderr.String())
			}
		})
	}
}

func TestPrompt_SemanticStyling(t *testing.T) {
	var stderr bytes.Buffer
	isTTY := true
	capturer := &capturer{
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
	}

	capturer.Prompt("Answer > ")
	expected := colorYellow + "Answer > " + colorReset
	if stderr.String() != expected {
		t.Errorf("expected %q, got %q", expected, stderr.String())
	}
}
