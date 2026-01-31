// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package agent

import (
	"bytes"
	"context"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/tools"
	"github.com/gosharplite/tell-me-go/internal/types"
)

func TestStreamResponse(t *testing.T) {
	sm := tools.NewSecurityManager()
	renderer := NewStdUIRenderer(sm)

	var stdout, stderr bytes.Buffer
	renderer.SetWriters(&stdout, &stderr)

	t.Run("Normal Streaming", func(t *testing.T) {
		stdout.Reset()
		stderr.Reset()
		ctx := context.Background()
		ch, finalize := renderer.StreamResponse(ctx, true, false)

		ch <- &types.Content{
			Parts: []*types.Part{
				{Thought: true, Text: "Thinking..."},
			},
		}
		ch <- &types.Content{
			Parts: []*types.Part{
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

		ch <- &types.Content{
			Parts: []*types.Part{
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

		ch <- &types.Content{
			Parts: []*types.Part{
				{
					InlineData: &types.Blob{
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
