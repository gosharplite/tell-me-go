// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestImageBlocks(t *testing.T) {
	tests := []struct {
		name  string
		parts []*llm.Part
		want  int // expected number of blocks
	}{
		{"single image", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47}}}}, 1},
		{"multiple images", []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}},
			{InlineData: &llm.Blob{MIMEType: "image/jpeg", Data: []byte{2}}},
		}, 2},
		{"no images", []*llm.Part{{Text: "hello"}}, 0},
		{"nil InlineData", []*llm.Part{{InlineData: nil}}, 0},
		{"empty data", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: nil}}}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := imageBlocks(tt.parts)
			if len(blocks) != tt.want {
				t.Errorf("got %d blocks, want %d", len(blocks), tt.want)
			}
			// Verify base64 URL format for image blocks
			for _, b := range blocks {
				ib, ok := b.(imageURLBlock)
				if !ok {
					t.Fatal("block is not imageURLBlock")
				}
				if ib.Type != "image_url" {
					t.Errorf("type = %q, want image_url", ib.Type)
				}
				if !strings.HasPrefix(ib.ImageURL.URL, "data:") {
					t.Errorf("URL doesn't start with data: %q", ib.ImageURL.URL)
				}
			}
		})
	}
}

func TestExtractImageParts(t *testing.T) {
	img := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}}
	txt := &llm.Part{Text: "hello"}
	parts := []*llm.Part{img, txt}
	images, rest := extractImageParts(parts)
	if len(images) != 1 || len(rest) != 1 {
		t.Errorf("got %d images, %d rest; want 1,1", len(images), len(rest))
	}
}

func TestVisionDisabled_KeepsStringContent(t *testing.T) {
	// Create a client with SupportsVision=false (default for deepseek)
	c := NewClient("https://api.deepseek.com", "deepseek-v4-flash",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	// Verify SupportsVision is false
	if c.capabilities.SupportsVision {
		t.Fatal("deepseek should not have SupportsVision")
	}

	// Build history with an image part — image should be dropped,
	// content stays as string.
	sink := &standardSink{}
	h := &llm.Content{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			{Text: "describe this"},
		},
	}
	var personaInjected bool
	err := c.appendMessagesFromHistoryItem(context.Background(), sink, h, nil, &personaInjected)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sink.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sink.messages))
	}
	msg := sink.messages[0]

	// Content should be a plain string (image was dropped)
	strContent, ok := msg.Content.(string)
	if !ok {
		t.Fatalf("expected string content, got %T", msg.Content)
	}
	if strContent != "describe this" {
		t.Errorf("expected 'describe this', got %q", strContent)
	}
}

func TestVision_KimiImagePayload(t *testing.T) {
	c := NewClient("https://api.moonshot.ai/v1", "kimi-k3",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	if !c.capabilities.SupportsVision {
		t.Fatal("kimi-k3 should have SupportsVision")
	}

	// Build history with an image part
	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			{Text: "describe this"},
		},
	}}

	msgs, err := c.toStandardMessages(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0]

	// Content must be []any (array), not string
	arr, ok := msg.Content.([]any)
	if !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 content blocks (image + text), got %d", len(arr))
	}

	// Marshal to JSON and verify exact shape
	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify image_url block is present
	if !strings.Contains(string(b), `"image_url"`) {
		t.Error("JSON payload missing image_url")
	}
	if !strings.Contains(string(b), `"type":"image_url"`) {
		t.Error("JSON payload missing type:image_url")
	}
	// Verify data URI
	if !strings.Contains(string(b), "data:image/png;base64") {
		t.Error("JSON payload missing data:image/png;base64 URI")
	}
	if !strings.Contains(string(b), `"type":"text"`) {
		t.Error("JSON payload missing text block")
	}
	if !strings.Contains(string(b), "describe this") {
		t.Error("JSON payload missing text content")
	}
}
