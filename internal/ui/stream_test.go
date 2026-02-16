// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ui

import (
	"bytes"
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
)

func TestStreamResponse(t *testing.T) {
	var stdout, stderr bytes.Buffer
	locker := &mockLocker{}
	renderer := NewRenderer(locker, &stdout, &stderr)

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
