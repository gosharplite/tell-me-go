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

func TestProcessStream_LoadingIndicator(t *testing.T) {
	var stdout, stderr bytes.Buffer
	locker := &mockLocker{}
	mc := &mockClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
	r := NewRenderer(locker, &stdout, &stderr, mc).(*stdUIRenderer)

	t.Run("Shows and clears indicator", func(t *testing.T) {
		stdout.Reset()
		ch := make(chan *llm.Content)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		state := &streamState{
			aggregated: &llm.Content{Role: "model"},
			isTerm:     true,
			rawOutput:  false,
		}
		ui := r.getUIState()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.processStream(ctx, ch, state, ui)
		}()

		// Wait for initial draw
		time.Sleep(50 * time.Millisecond)
		if !strings.Contains(stdout.String(), "Thinking...") {
			t.Error("expected stdout to contain 'Thinking...'")
		}

		// Send content
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Done"}}}
		close(ch)
		wg.Wait()

		// Should contain Done
		if !strings.Contains(stdout.String(), "Done") {
			t.Errorf("expected stdout to contain 'Done', got %q", stdout.String())
		}

		// Should contain clear sequence (either restore cursor + clear forward or clear line)
		hasRestore := strings.Contains(stdout.String(), "\x1b8") || strings.Contains(stdout.String(), "\0338")
		hasClearLine := strings.Contains(stdout.String(), "\x1b[2K") || strings.Contains(stdout.String(), "\033[2K")

		if !hasRestore && !hasClearLine {
			t.Errorf("expected stdout to contain clear sequence, got %q", stdout.String())
		}
	})

	t.Run("Shows and clears indicator in raw mode", func(t *testing.T) {
		stdout.Reset()
		ch := make(chan *llm.Content)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		state := &streamState{
			aggregated: &llm.Content{Role: "model"},
			isTerm:     true,
			rawOutput:  true,
		}
		ui := r.getUIState()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.processStream(ctx, ch, state, ui)
		}()

		time.Sleep(50 * time.Millisecond)
		ch <- &llm.Content{Parts: []*llm.Part{{Text: "Done"}}}
		close(ch)
		wg.Wait()

		if !strings.Contains(stdout.String(), "Done") {
			t.Errorf("expected stdout to contain 'Done', got %q", stdout.String())
		}

		// In raw mode it should use clear line escape
		if !strings.Contains(stdout.String(), "\x1b[2K") && !strings.Contains(stdout.String(), "\033[2K") {
			t.Errorf("expected stdout to contain clear line escape (\\033[2K), got %q", stdout.String())
		}
	})

	t.Run("Clears indicator on context cancellation", func(t *testing.T) {
		stdout.Reset()
		ch := make(chan *llm.Content)
		ctx, cancel := context.WithCancel(context.Background())

		state := &streamState{
			aggregated: &llm.Content{Role: "model"},
			isTerm:     true,
			rawOutput:  false,
		}
		ui := r.getUIState()

		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.processStream(ctx, ch, state, ui)
		}()

		time.Sleep(50 * time.Millisecond)
		cancel() // Cancel context
		wg.Wait()

		// Should contain clear sequence
		hasRestore := strings.Contains(stdout.String(), "\x1b8") || strings.Contains(stdout.String(), "\0338")
		hasClearLine := strings.Contains(stdout.String(), "\x1b[2K") || strings.Contains(stdout.String(), "\033[2K")

		if !hasRestore && !hasClearLine {
			t.Errorf("expected stdout to contain clear sequence on cancel, got %q", stdout.String())
		}
	})
}
