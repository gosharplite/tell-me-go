// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"github.com/gosharplite/tell-me-go/internal/agent/orchestration"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCapturePromptContextCancellation(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	capturer := &capturer{
		Stdin:  pr,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.CapturePrompt(ctx, fs)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPrompt_Pipe(t *testing.T) {
	t.Parallel()
	inputStr := "hello from pipe"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)

	prompt, err := capturer.CapturePrompt(context.Background(), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != inputStr {
		t.Errorf("expected %q, got %q", inputStr, prompt)
	}
}

func TestPrompt_Args(t *testing.T) {
	t.Parallel()
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"hello", "world"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", prompt)
	}
}

func TestPrompt_Empty(t *testing.T) {
	t.Parallel()
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	_, err := capturer.CapturePrompt(context.Background(), fs)
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestPrompt_SkipTTYWaitEmpty(t *testing.T) {
	t.Parallel()
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt, err := capturer.CapturePrompt(context.Background(), fs, orchestration.WithSkipTTYWait(true))

	if !errors.Is(err, ErrNoInput) {
		t.Errorf("expected ErrNoInput, got %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

func TestPrompt_MockEnv(t *testing.T) {
	t.Parallel()
	capturer := &capturer{
		Stdin:      strings.NewReader(""),
		Stdout:     io.Discard,
		Stderr:     io.Discard,
		Clock:      &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		mockPrompt: "mocked prompt",
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	prompt, err := capturer.CapturePrompt(context.Background(), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "mocked prompt" {
		t.Errorf("expected 'mocked prompt', got %q", prompt)
	}
}

func TestPrompt_EmptyPipe(t *testing.T) {
	t.Parallel()
	// Empty stdin (simulated pipe)

	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial", "prompt"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should return the initial prompt from args
	if prompt != "initial prompt" {
		t.Errorf("expected 'initial prompt', got %q", prompt)
	}
}

func TestPrintFeedback_NoSM(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	capturer := &capturer{
		Stdin:  strings.NewReader(""),
		Stdout: &buf,
		Stderr: io.Discard,
		SM:     nil, // No security manager
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	// Should not panic and should print the message
	capturer.printFeedback(&buf, false, "", "test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected output to contain 'test message', got %q", buf.String())
	}
}

func TestPrompt_Combined(t *testing.T) {
	t.Parallel()
	inputStr := "pipe input"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	if err := fs.Parse([]string{"initial"}); err != nil {
		t.Fatal(err)
	}

	prompt, err := capturer.CapturePrompt(context.Background(), fs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "initial\npipe input"
	if prompt != expected {
		t.Errorf("expected %q, got %q", expected, prompt)
	}
}

func TestIsTTY_False(t *testing.T) {
	t.Parallel()
	capturer := &capturer{}
	if capturer.IsTTY("not a file") {
		t.Error("expected IsTTY to be false for string")
	}
}

func TestCaptureFromTTY(t *testing.T) {
	t.Parallel()
	inputStr := "tty input"
	capturer := &capturer{
		Stdin:  strings.NewReader(inputStr),
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
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
	t.Parallel()
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	capturer := &capturer{
		Stdin:  pr,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := capturer.captureFromTTY(ctx, false)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestWarn_SemanticStyling(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	isTTY := true
	mc := &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
	capturer := &capturer{
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
		Clock:         mc,
	}

	tests := []struct {
		name     string
		message  string
		color    string
		expected string
	}{
		{
			name:     "Security warning",
			message:  "[SECURITY] High risk",
			color:    colorRed,
			expected: colorRed + "[12:00:00] [SECURITY] High risk" + colorReset + "\n",
		},
		{
			name:     "Normal warning",
			message:  "Regular warning",
			color:    colorYellow,
			expected: colorYellow + "[12:00:00] Regular warning" + colorReset + "\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stderr.Reset()
			capturer.Warn(tt.message)
			got := stderr.String()

			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestConfirm_SemanticStyling(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	isTTY := true
	capturer := &capturer{
		Stdin:         strings.NewReader(""),
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
		mockAnswer:    "y",
		Clock:         &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
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
	t.Parallel()
	var stderr bytes.Buffer
	isTTY := true
	capturer := &capturer{
		Stderr:        &stderr,
		isTTYOverride: &isTTY,
		Clock:         &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
	}

	capturer.Prompt("Answer > ")
	expected := colorYellow + "Answer > " + colorReset
	if stderr.String() != expected {
		t.Errorf("expected %q, got %q", expected, stderr.String())
	}
}

func TestCapturer_Confirm_ContextCancelled(t *testing.T) {
	t.Parallel()
	// 1. Pre-cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 2. Setup capturer with dummy streams (io.Discard or similar)
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	c := &capturer{
		Stdin:  pr,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		reader: bufio.NewReader(pr),
	}

	// 3. Execute the blocking call
	result, err := c.Confirm(ctx, "Proceed?")

	// 4. Architectural mandate: Must return context.Canceled immediately
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if result != false {
		t.Error("expected false result on cancellation")
	}
}

func TestCapturer_ReadLine_ContextCancelled(t *testing.T) {
	t.Parallel()
	// 1. Pre-cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// 2. Setup capturer with dummy streams (io.Discard or similar)
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	c := &capturer{
		Stdin:  pr,
		Stdout: io.Discard,
		Stderr: io.Discard,
		Clock:  &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)},
		reader: bufio.NewReader(pr),
	}

	// 3. Execute the blocking call
	result, err := c.ReadLine(ctx)

	// 4. Architectural mandate: Must return context.Canceled immediately
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on cancellation, got %q", result)
	}
}

func TestCapturer_ReadLine_ContextCancellation_Concurrency(t *testing.T) {
	t.Parallel()

	// 1. Create a pipe that blocks forever because nothing will write to it
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		_ = pr.Close()
		_ = pw.Close()
	})

	// 2. Setup capturer with the blocking reader using the constructor
	c := NewCapturer(pr, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "")

	// 3. Create a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)

	// 4. Execute the blocking read in a goroutine
	go func() {
		_, err := c.ReadLine(ctx)
		errCh <- err
	}()

	// 5. Wait for the context to cancel the read, or fail the test if it hangs
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			t.Errorf("expected context cancellation error, got: %v", err)
		}
	case <-timer.C:
		t.Fatal("Test timed out: ReadLine did not respect context cancellation")
	}
}

func TestCapturer_ReadSingleKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mockAnswer string
		setupInput func(t *testing.T) io.Reader
		setupCtx   func() (context.Context, context.CancelFunc)
		want       string
		wantErr    bool
	}{
		{
			name:       "fast return with mockAnswer",
			mockAnswer: "Yes",
			setupInput: func(t *testing.T) io.Reader {
				return nil // Shouldn't be read
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want:    "y",
			wantErr: false,
		},
		{
			name:       "context cancellation triggers readByteFallback",
			mockAnswer: "",
			setupInput: func(t *testing.T) io.Reader {
				pr, pw := io.Pipe()
				t.Cleanup(func() {
					_ = pr.Close()
					_ = pw.Close()
				})
				// It's an io.Reader but not an *os.File, so it triggers fallback immediately
				return pr
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel() // Cancel immediately
				return ctx, cancel
			},
			want:    "",
			wantErr: true,
		},
		{
			name:       "non-TTY os.File triggers readByteFallback and reads byte",
			mockAnswer: "",
			setupInput: func(t *testing.T) io.Reader {
				f, err := os.CreateTemp(t.TempDir(), "test-stdin-*")
				if err != nil {
					t.Fatalf("failed to create temp file: %v", err)
				}
				t.Cleanup(func() { _ = f.Close() })

				_, err = f.Write([]byte("x"))
				if err != nil {
					t.Fatalf("failed to write to temp file: %v", err)
				}
				_, err = f.Seek(0, 0) // rewind to beginning
				if err != nil {
					t.Fatalf("failed to seek: %v", err)
				}
				return f
			},
			setupCtx: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want:    "x",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			stdin := tt.setupInput(t)
			ctx, cancel := tt.setupCtx()
			t.Cleanup(cancel)

			// Construct Capturer with isolated Stdin
			// Using correct NewCapturer signature:
			// func NewCapturer(stdin io.Reader, stdout, stderr io.Writer, sm domain_security.ISecurityManager, clk clock.Clock, mockPrompt, mockAnswer string) domain_security.UserInteractor
			c := NewCapturer(stdin, io.Discard, io.Discard, nil, nil, "", tt.mockAnswer)

			got, err := c.ReadSingleKey(ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReadSingleKey() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("ReadSingleKey() got = %v, want %v", got, tt.want)
			}
		})
	}
}
