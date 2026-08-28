// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// mustEncodePNG encodes an w×h RGBA image as PNG. Real encoders live here —
// never in client_vision_test.go (pins must not move).
func mustEncodePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatalf("encode PNG: %v", err)
	}
	return buf.Bytes()
}

func mustEncodeJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatalf("encode JPEG: %v", err)
	}
	return buf.Bytes()
}

func mustEncodeGIF(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatalf("encode GIF: %v", err)
	}
	return buf.Bytes()
}

func TestImageLongestEdge_Formats(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want int
	}{
		{"png 1x1", mustEncodePNG(t, 1, 1), 1},
		{"jpeg 1x1", mustEncodeJPEG(t, 1, 1), 1},
		{"gif 1x1", mustEncodeGIF(t, 1, 1), 1},
		{"tall png 1x3 longest edge is height", mustEncodePNG(t, 1, 3), 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edge, err := imageLongestEdge(tt.data)
			if err != nil {
				t.Fatalf("imageLongestEdge(%s): %v", tt.name, err)
			}
			if edge != tt.want {
				t.Errorf("imageLongestEdge(%s) = %d, want %d", tt.name, edge, tt.want)
			}
		})
	}
}

func TestImageLongestEdge_Undecodable(t *testing.T) {
	if _, err := imageLongestEdge([]byte("not an image")); err == nil {
		t.Error("expected error for undecodable bytes, got nil")
	}
}

func TestCheckResponsesImageDimensions(t *testing.T) {
	c := &client{}
	tests := []struct {
		name    string
		parts   []*llm.Part
		wantErr bool
	}{
		{"png 1x1", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 1, 1)}}}, false},
		{"png 2048x2048 at cap", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 2048, 2048)}}}, false},
		{"png 2049x2049 over cap", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 2049, 2049)}}}, true},
		{"aggregate 1x1 and 2049x2049", []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 1, 1)}},
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 2049, 2049)}},
		}, true},
		{"undecodable bytes skipped", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("not an image")}}}, false},
		{"non-image part ignored", []*llm.Part{{Text: "hello"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := c.checkResponsesImageDimensions(tt.parts)
			if tt.wantErr {
				assertMediaSizeError(t, err, 2048, 2049)
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// assertMediaSizeError verifies the typed MediaSizeError contract for a
// longest-edge violation: Kind/Mode/Cap/Actual, the ErrTerminal unwrap
// chain, and the pre-rendered message containing both cap and actual.
func assertMediaSizeError(t *testing.T, err error, cap, actual int) {
	t.Helper()
	var mse *llm.MediaSizeError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *llm.MediaSizeError, got %T", err)
	}
	if mse.Kind != llm.MediaSizePerImage {
		t.Errorf("Kind = %d, want MediaSizePerImage", mse.Kind)
	}
	if mse.Mode != llm.MediaSizeModeLongestEdge {
		t.Errorf("Mode = %d, want MediaSizeModeLongestEdge", mse.Mode)
	}
	if mse.Cap != cap {
		t.Errorf("Cap = %d, want %d", mse.Cap, cap)
	}
	if mse.Actual != actual {
		t.Errorf("Actual = %d, want %d", mse.Actual, actual)
	}
	if !errors.Is(err, llm.ErrTerminal) {
		t.Errorf("errors.Is(err, llm.ErrTerminal) = false, want true")
	}
	if !strings.Contains(err.Error(), "2048") || !strings.Contains(err.Error(), "2049") {
		t.Errorf("Error() = %q, want to contain 2048 and 2049", err.Error())
	}
}

func TestResponsesDimensionGuard_GPT54FailsLoud(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"resp_1","output":[{"type":"message","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}],"usage":{"total_tokens":10}}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "gpt-5.4", &auth.BearerAuth{Token: "test"})

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 3000, 1)}},
		},
	}}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected dimension-guard error, got nil")
	}
	var mse *llm.MediaSizeError
	if !errors.As(err, &mse) {
		t.Fatalf("expected *llm.MediaSizeError, got %T", err)
	}
	if mse.Actual != 3000 {
		t.Errorf("Actual = %d, want 3000", mse.Actual)
	}
	if requestCount != 0 {
		t.Errorf("expected zero HTTP requests (guard fires before network I/O), got %d", requestCount)
	}
}

func TestResponsesDimensionGuard_GPT50Unguarded(t *testing.T) {
	server, capture := newRoutingCaptureServer(t)
	c := NewClient(server.URL, "gpt-5.0", &auth.BearerAuth{Token: "test"})

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: mustEncodePNG(t, 3000, 1)}},
		},
	}}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err != nil {
		t.Fatalf("SendChat failed (gpt-5.0 chat path must be unguarded): %v", err)
	}
	if capture.path != "/chat/completions" {
		t.Errorf("expected /chat/completions, got %s", capture.path)
	}
	if !strings.Contains(capture.body, `"type":"image_url"`) {
		t.Errorf("body missing image_url block: %s", capture.body)
	}
}
