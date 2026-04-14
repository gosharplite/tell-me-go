// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

func TestCapturePromptContextCancellation(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()

	capturer := NewCapturer(pr, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := capturer.CapturePrompt(ctx, nil)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestPrompt_Pipe(t *testing.T) {
	t.Parallel()
	inputStr := "hello from pipe"
	capturer := NewCapturer(strings.NewReader(inputStr), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != inputStr {
		t.Errorf("expected %q, got %q", inputStr, prompt)
	}
}

func TestPrompt_Args(t *testing.T) {
	t.Parallel()
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), []string{"hello", "world"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if prompt != "hello world" {
		t.Errorf("expected 'hello world', got %q", prompt)
	}
}

func TestPrompt_Empty(t *testing.T) {
	t.Parallel()
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	_, err := capturer.CapturePrompt(context.Background(), nil)
	if err == nil {
		t.Error("expected error for empty prompt, got nil")
	}
}

func TestPrompt_SkipTTYWaitEmpty(t *testing.T) {
	t.Parallel()
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), nil, ports.WithSkipTTYWait(true))

	if !errors.Is(err, ErrNoInput) {
		t.Errorf("expected ErrNoInput, got %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

func TestPrompt_MockEnv(t *testing.T) {
	t.Parallel()
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "mocked prompt", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), nil)
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

	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), []string{"initial", "prompt"})
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
	capturer := NewCapturer(strings.NewReader(""), &buf, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	// Should not panic and should print the message
	capturer.printFeedback(&buf, false, "", "test message")
	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected output to contain 'test message', got %q", buf.String())
	}
}

func TestPrompt_Combined(t *testing.T) {
	t.Parallel()
	inputStr := "pipe input"
	capturer := NewCapturer(strings.NewReader(inputStr), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	prompt, err := capturer.CapturePrompt(context.Background(), []string{"initial"})
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
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	if capturer.IsTTY("not a file") {
		t.Error("expected IsTTY to be false for string")
	}
}

func TestCaptureFromTTY_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T) (io.Reader, io.Closer)
		ctxFunc func() (context.Context, context.CancelFunc)
		want    string
		wantErr error
	}{
		{
			name: "Multi-line Input",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				go func() {
					_, _ = pw.Write([]byte("line 1\nline 2"))
					_ = pw.Close()
				}()
				return pr, nil // closer handled by goroutine or t.Cleanup if we wanted but pw.Close() is key
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: "line 1\nline 2",
		},
		{
			name: "Empty Input",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				_ = pw.Close()
				return pr, nil
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: "",
		},
		{
			name: "Context Cancellation",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				t.Cleanup(func() {
					if err := pr.Close(); err != nil {
						t.Logf("failed to close pipe reader: %v", err)
					}
					if err := pw.Close(); err != nil {
						t.Logf("failed to close pipe writer: %v", err)
					}
				})
				return pr, nil
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
				return ctx, cancel
			},
			wantErr: context.DeadlineExceeded,
		},
		{
			name: "Input Size Limit",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				go func() {
					// Write more than 1MB
					data := make([]byte, 1024*1024+100)
					for i := range data {
						data[i] = 'A'
					}
					_, _ = pw.Write(data)
					_ = pw.Close()
				}()
				return pr, nil
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: strings.Repeat("A", 1024*1024), // Should be truncated to maxPromptSize
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdin, closer := tt.setup(t)

			ctx, cancel := tt.ctxFunc()
			defer cancel()

			c := NewCapturer(stdin, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", false).(*capturer)
			t.Cleanup(func() {
				if err := c.Close(context.Background()); err != nil {
					t.Logf("failed to close capturer: %v", err)
				}
			})

			if closer != nil {
				t.Cleanup(func() {
					if err := closer.Close(); err != nil {
						t.Logf("failed to close: %v", err)
					}
				})
			}
			if f, ok := stdin.(*os.File); ok {
				t.Cleanup(func() {
					if err := f.Close(); err != nil {
						t.Logf("failed to close file: %v", err)
					}
				})
			}

			got, err := c.captureFromTTY(ctx, false)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got length %d, want %d", len(got), len(tt.want))
				if len(got) < 100 {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

func TestWarn_SemanticStyling(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	isTTY := true
	mc := &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}
	capturer := NewCapturer(strings.NewReader(""), io.Discard, &stderr, nil, mc, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	capturer.isTTYOverride = &isTTY

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
	capturer := NewCapturer(strings.NewReader(""), io.Discard, &stderr, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "y", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	capturer.isTTYOverride = &isTTY

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
	capturer := NewCapturer(strings.NewReader(""), io.Discard, &stderr, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := capturer.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	capturer.isTTYOverride = &isTTY

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

	c := NewCapturer(pr, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

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

	c := NewCapturer(pr, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

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

	// 2. Setup capturer with the blocking reader using the constructor
	c := NewCapturer(pr, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

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

func TestReadSingleKey_Comprehensive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                   string
		mockAnswer             string
		isTTYOverride          *bool
		disableEscapeSequences bool
		setup                  func(t *testing.T) (io.Reader, io.Closer)
		ctxFunc                func() (context.Context, context.CancelFunc)
		want                   string
		wantErr                string
	}{
		{
			name:       "Mock Answer",
			mockAnswer: "Yes",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				return nil, nil
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: "y",
		},
		{
			name:          "Non-TTY Pipe",
			isTTYOverride: boolPtr(false),
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				return pr, pw
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			wantErr: "confirmation required but not running in a terminal",
		},
		{
			name:          "Simulated TTY Pipe",
			isTTYOverride: boolPtr(true),
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				_, _ = pw.Write([]byte("K"))
				return pr, pw
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: "k",
		},
		{
			name: "Context Cancellation",
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				return pr, pw
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
			wantErr: context.Canceled.Error(),
		},
		{
			name:          "Ctrl+C (ETX)",
			isTTYOverride: boolPtr(true),
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				_, _ = pw.Write([]byte{3})
				return pr, pw
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want:    "k", // Wait, this is wrong in original test but I'll fix it to wantErr: context.Canceled.Error() below
			wantErr: context.Canceled.Error(),
		},
		{
			name:                   "Fallback when not TTY but escape sequences disabled",
			isTTYOverride:          boolPtr(false),
			disableEscapeSequences: true,
			setup: func(t *testing.T) (io.Reader, io.Closer) {
				pr, pw, _ := os.Pipe()
				_, _ = pw.Write([]byte("Z"))
				return pr, pw
			},
			ctxFunc: func() (context.Context, context.CancelFunc) {
				return context.WithCancel(context.Background())
			},
			want: "z",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdin, closer := tt.setup(t)

			ctx, cancel := tt.ctxFunc()
			defer cancel()

			c := NewCapturer(stdin, io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", tt.mockAnswer, tt.disableEscapeSequences).(*capturer)
			t.Cleanup(func() {
				if err := c.Close(context.Background()); err != nil {
					t.Logf("failed to close capturer: %v", err)
				}
			})

			if closer != nil {
				t.Cleanup(func() {
					if err := closer.Close(); err != nil {
						t.Logf("failed to close: %v", err)
					}
				})
			}
			if f, ok := stdin.(*os.File); ok {
				t.Cleanup(func() {
					if err := f.Close(); err != nil {
						t.Logf("failed to close file: %v", err)
					}
				})
			}
			c.isTTYOverride = tt.isTTYOverride

			got, err := c.ReadSingleKey(ctx)
			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.wantErr)
				} else if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func boolPtr(b bool) *bool { return &b }

func TestCapturer_ReadLine_Success(t *testing.T) {
	t.Parallel()
	input := "line one\nline two\n"
	c := NewCapturer(strings.NewReader(input), io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	line, err := c.ReadLine(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line != "line one\n" {
		t.Errorf("expected 'line one\\n', got %q", line)
	}

	line, err = c.ReadLine(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line != "line two\n" {
		t.Errorf("expected 'line two\\n', got %q", line)
	}
}

func TestCapturer_ReadLine_EOF(t *testing.T) {
	t.Parallel()
	input := "incomplete"
	c := NewCapturer(strings.NewReader(input), io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	line, err := c.ReadLine(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if line != "incomplete" {
		t.Errorf("expected 'incomplete', got %q", line)
	}

	line, err = c.ReadLine(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
	if line != "" {
		t.Errorf("expected empty string, got %q", line)
	}
}

func TestCapturer_RequestAfterClose(t *testing.T) {
	t.Parallel()
	c := NewCapturer(strings.NewReader("data"), io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	err := c.Close(context.Background())
	if err != nil {
		t.Fatalf("failed to close: %v", err)
	}

	_, err = c.ReadLine(context.Background())
	if !errors.Is(err, errCapturerClosed) {
		t.Errorf("ReadLine: expected errCapturerClosed, got %v", err)
	}

	_, err = c.Confirm(context.Background(), "Proceed?")
	if !errors.Is(err, errCapturerClosed) {
		t.Errorf("Confirm: expected errCapturerClosed, got %v", err)
	}

	isTTY := false
	c.isTTYOverride = &isTTY
	_, err = c.CapturePrompt(context.Background(), nil)
	if !errors.Is(err, errCapturerClosed) {
		t.Errorf("CapturePrompt: expected errCapturerClosed, got %v", err)
	}
}

func TestCapturer_ReadLine_ContextCancelled_BlockingSend(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()

	c := NewCapturer(pr, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

	// Action 1: Start a ReadLine call that will block in the worker
	// The worker will be busy reading from 'pr'.
	go func() {
		_, _ = c.ReadLine(context.Background())
	}()

	// Give the goroutine a moment to send the request and start reading
	time.Sleep(50 * time.Millisecond)

	// Action 2: Call ReadLine with a pre-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ReadLine(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestCapturer_Close_Idempotent(t *testing.T) {
	t.Parallel()
	c := NewCapturer(strings.NewReader("data"), io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)

	err := c.Close(context.Background())
	if err != nil {
		t.Fatalf("first close failed: %v", err)
	}

	err = c.Close(context.Background())
	if err != nil {
		t.Fatalf("second close failed: %v", err)
	}
}

func TestCapturer_Close_ContextCancelled(t *testing.T) {
	t.Parallel()
	// Using a pipe that is never closed by the worker because it's stuck reading.
	pr, pw := io.Pipe()
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

	c := NewCapturer(pr, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)

	// Action: Start a ReadLine call that will block in the worker
	go func() {
		_, _ = c.ReadLine(context.Background())
	}()

	// Give the goroutine a moment to send the request and start reading
	time.Sleep(50 * time.Millisecond)

	// Now try to close with a pre-cancelled context.
	// Close will close requestChan, but the worker is stuck in ReadString and won't see the close until it finishes the read.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := c.Close(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestCapturer_ReadSingleKey_ContextCancelled_AfterSend(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()

	c := NewCapturer(pr, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := c.ReadSingleKey(ctx)
		errCh <- err
	}()

	// Give it time to send the request
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timed out waiting for ReadSingleKey to return after cancellation")
	}
}

func TestCapturer_ReadSingleKey_ContextCancelled_BlockingSend(t *testing.T) {
	t.Parallel()
	pr, pw := io.Pipe()

	c := NewCapturer(pr, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})
	t.Cleanup(func() {
		if err := pw.Close(); err != nil {
			t.Logf("failed to close pipe writer: %v", err)
		}
		if err := pr.Close(); err != nil {
			t.Logf("failed to close pipe reader: %v", err)
		}
	})

	// Block the worker
	go func() {
		_, _ = c.ReadLine(context.Background())
	}()
	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.ReadSingleKey(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestCapturer_ReadLine_IOError(t *testing.T) {
	t.Parallel()
	c := NewCapturer(&uiErrorReader{}, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	_, err := c.ReadLine(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read error") {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestCapturer_ReadSingleKey_IOError(t *testing.T) {
	t.Parallel()
	c := NewCapturer(&uiErrorReader{}, io.Discard, io.Discard, nil, nil, "", "", true).(*capturer)
	t.Cleanup(func() {
		if err := c.Close(context.Background()); err != nil {
			t.Logf("failed to close capturer: %v", err)
		}
	})

	_, err := c.ReadSingleKey(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "read error") {
		t.Errorf("expected read error, got %v", err)
	}
}

func TestPrompt_InteractiveEmpty(t *testing.T) {
	t.Parallel()
	// Simulate empty interactive input via TTY
	capturer := NewCapturer(strings.NewReader(""), io.Discard, io.Discard, nil, &mockClock{now: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)}, "", "", true).(*capturer)
	t.Cleanup(func() {
		_ = capturer.Close(context.Background())
	})
	
	// Override IsTTY to true
	isTTY := true
	capturer.isTTYOverride = &isTTY

	prompt, err := capturer.CapturePrompt(context.Background(), nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}
