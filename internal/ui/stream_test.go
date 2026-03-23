// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/pkg/clock"
)

func TestStreamResponse(t *testing.T) {
	var stdout, stderr bytes.Buffer
	locker := &mockLocker{}
	renderer := NewRenderer(locker, &stdout, &stderr, clock.RealClock{})

	t.Run("Normal Streaming", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := renderer.StreamResponse(ctx, true, false)

		ch <- &llm.Content{
			Parts: []*llm.Part{
				{IsThought: true, Text: "Thinking..."},
			},
		}
		ch <- &llm.Content{
			Parts: []*llm.Part{
				{Text: "Hello, world!"},
			},
		}

		aggregated := finalize()

		if len(aggregated.Parts) != 2 {
			t.Errorf("expected 2 parts, got %d", len(aggregated.Parts))
		}
	})

	t.Run("Raw Output", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := renderer.StreamResponse(ctx, true, true)

		ch <- &llm.Content{
			Parts: []*llm.Part{
				{Text: "Raw text"},
			},
		}

		finalize()

		if stdout.String() != "Raw text" {
			t.Errorf("expected 'Raw text', got %q", stdout.String())
		}
	})

	t.Run("Inline Data", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := renderer.StreamResponse(ctx, true, false)

		ch <- &llm.Content{
			Parts: []*llm.Part{
				{
					InlineData: &llm.Blob{
						MIMEType: "image/png",
						Data:     []byte("fake data"),
					},
				},
			},
		}

		finalize()

		if !bytes.Contains(stderr.Bytes(), []byte("[Media] image/png")) {
			t.Errorf("expected stderr to contain '[Media] image/png', got %q", stderr.String())
		}
	})
}

func setupLoadingIndicatorTest(t *testing.T) (*stdUIRenderer, *bytes.Buffer, *mockLocker) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)
	return r, &stdout, locker
}

func readStdout(locker *mockLocker, stdout *bytes.Buffer) string {
	locker.TerminalLock()
	defer locker.TerminalUnlock()
	return stdout.String()
}

func waitForOutput(t *testing.T, locker *mockLocker, stdout *bytes.Buffer, expected string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(readStdout(locker, stdout), expected) {
			return // Success
		}
		time.Sleep(10 * time.Millisecond) // Short poll interval
	}
	t.Errorf("timeout waiting for %q in stdout, got: %q", expected, readStdout(locker, stdout))
}

func TestProcessStream_LoadingIndicator(t *testing.T) {
	t.Run("Shows and clears indicator", testLoadingIndicator_Normal)
	t.Run("Shows and clears indicator in raw mode", testLoadingIndicator_RawMode)
	t.Run("Clears indicator on context cancellation", testLoadingIndicator_Cancellation)
}

func testLoadingIndicator_Normal(t *testing.T) {
	r, stdout, locker := setupLoadingIndicatorTest(t)
	ch := make(chan *llm.Content)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := &streamState{aggregated: &llm.Content{Role: "model"}, isTerm: true}
	ui := r.getUIState()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.processStream(ctx, ch, state, ui)
	}()

	waitForOutput(t, locker, stdout, "Thinking...", 1*time.Second)
	ch <- &llm.Content{Parts: []*llm.Part{{Text: "Done"}}}
	close(ch)
	wg.Wait()

	out := readStdout(locker, stdout)
	if !strings.Contains(out, "Done") {
		t.Errorf("expected stdout to contain 'Done', got %q", out)
	}
	if !strings.Contains(out, "\x1b8") && !strings.Contains(out, "\0338") && !strings.Contains(out, "\x1b[2K") && !strings.Contains(out, "\033[2K") {
		t.Errorf("expected stdout to contain clear sequence, got %q", out)
	}
}

func testLoadingIndicator_RawMode(t *testing.T) {
	r, stdout, locker := setupLoadingIndicatorTest(t)
	ch := make(chan *llm.Content)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	state := &streamState{aggregated: &llm.Content{Role: "model"}, isTerm: true, rawOutput: true}
	ui := r.getUIState()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.processStream(ctx, ch, state, ui)
	}()

	waitForOutput(t, locker, stdout, "Thinking...", 1*time.Second)
	ch <- &llm.Content{Parts: []*llm.Part{{Text: "Done"}}}
	close(ch)
	wg.Wait()

	out := readStdout(locker, stdout)
	if !strings.Contains(out, "Done") {
		t.Errorf("expected stdout to contain 'Done', got %q", out)
	}
	if !strings.Contains(out, "\x1b[2K") && !strings.Contains(out, "\033[2K") {
		t.Errorf("expected stdout to contain clear line escape (\\033[2K), got %q", out)
	}
}

func testLoadingIndicator_Cancellation(t *testing.T) {
	r, stdout, locker := setupLoadingIndicatorTest(t)
	ch := make(chan *llm.Content)
	ctx, cancel := context.WithCancel(context.Background())

	state := &streamState{aggregated: &llm.Content{Role: "model"}, isTerm: true}
	ui := r.getUIState()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.processStream(ctx, ch, state, ui)
	}()

	waitForOutput(t, locker, stdout, "Thinking...", 1*time.Second)
	cancel()
	wg.Wait()

	out := readStdout(locker, stdout)
	if !strings.Contains(out, "\x1b8") && !strings.Contains(out, "\0338") && !strings.Contains(out, "\x1b[2K") && !strings.Contains(out, "\033[2K") {
		t.Errorf("expected stdout to contain clear sequence on cancel, got %q", out)
	}
}
