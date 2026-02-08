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
	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.Prompt(ctx, fs, 0, false)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPrompt_Pipe(t *testing.T) {
	inputStr := "hello from pipe"
	capturer := &Capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	prompt, err := capturer.Prompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != inputStr {
		t.Errorf("expected %q, got %q", inputStr, prompt)
	}
}

func TestPrompt_Args(t *testing.T) {
	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.Prompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", prompt)
	}
}

func TestPrompt_Empty(t *testing.T) {
	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.Prompt(context.Background(), fs, 0, false)
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestPrompt_MockEnv(t *testing.T) {
	os.Setenv("TELL_ME_MOCK_PROMPT", "mocked prompt")
	defer os.Unsetenv("TELL_ME_MOCK_PROMPT")

	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt, err := capturer.Prompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "mocked prompt" {
		t.Errorf("expected 'mocked prompt', got %q", prompt)
	}
}

func TestPrompt_EmptyPipe(t *testing.T) {
	// Empty stdin (simulated pipe)
	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial", "prompt"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.Prompt(context.Background(), fs, 0, false)
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
	capturer := &Capturer{
		Stdin:  strings.NewReader(""),
		Stdout: &buf,
		Stderr: io.Discard,
		SM:     nil, // No security manager
	}

	// Should not panic and should print the message
	capturer.PrintFeedback(&buf, false, "", "test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected output to contain 'test message', got %q", buf.String())
	}
}

func TestPrompt_Combined(t *testing.T) {
	inputStr := "pipe input"
	capturer := &Capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.Prompt(context.Background(), fs, 0, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "initial\npipe input"
	if prompt != expected {
		t.Errorf("expected %q, got %q", expected, prompt)
	}
}

func TestIsTTY_False(t *testing.T) {
	capturer := &Capturer{}
	if capturer.IsTTY("not a file") {
		t.Error("expected IsTTY to be false for string")
	}
}

func TestCaptureFromTTY(t *testing.T) {
	inputStr := "tty input"
	capturer := &Capturer{
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
	capturer := &Capturer{
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
