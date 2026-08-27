// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestMediaBlocks(t *testing.T) {
	tests := []struct {
		name  string
		parts []*llm.Part
		caps  llm.Capabilities
		want  int // expected number of blocks
	}{
		{"single image", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 1},
		{"multiple images", []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}},
			{InlineData: &llm.Blob{MIMEType: "image/jpeg", Data: []byte{2}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 2},
		{"single video", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{0x00, 0x00, 0x00}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 1},
		{"mixed image and video", []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}},
			{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{2}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 2},
		{"no media", []*llm.Part{{Text: "hello"}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 0},
		{"nil InlineData", []*llm.Part{{InlineData: nil}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 0},
		{"empty data", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: nil}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 0},
		{"pdf skipped", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 0},
		{"audio skipped", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "audio/mp3", Data: []byte{2}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: true}, 0},
		{"vision-without-video skips video", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{0x00}}}}, llm.Capabilities{SupportsVision: true, SupportsVideo: false}, 0},
		{"image dropped without vision", []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89}}}}, llm.Capabilities{SupportsVision: false, SupportsVideo: true}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			blocks := mediaBlocks(tt.parts, nil, tt.caps)
			if len(blocks) != tt.want {
				t.Errorf("got %d blocks, want %d", len(blocks), tt.want)
			}
			// Verify URL format for each block type
			for _, b := range blocks {
				switch block := b.(type) {
				case imageURLBlock:
					if block.Type != "image_url" {
						t.Errorf("image type = %q, want image_url", block.Type)
					}
					if !strings.HasPrefix(block.ImageURL.URL, "data:") {
						t.Errorf("image URL doesn't start with data: %q", block.ImageURL.URL)
					}
				case videoURLBlock:
					if block.Type != "video_url" {
						t.Errorf("video type = %q, want video_url", block.Type)
					}
					if !strings.HasPrefix(block.VideoURL.URL, "data:") {
						t.Errorf("video URL doesn't start with data: %q", block.VideoURL.URL)
					}
				default:
					t.Fatalf("block is not imageURLBlock or videoURLBlock: %T", b)
				}
			}
		})
	}
}

func TestExtractMediaParts(t *testing.T) {
	img := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}}
	txt := &llm.Part{Text: "hello"}
	pdf := &llm.Part{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}}
	audio := &llm.Part{InlineData: &llm.Blob{MIMEType: "audio/mp3", Data: []byte{2}}}
	parts := []*llm.Part{img, txt, pdf, audio}
	media, rest := extractMediaParts(parts)
	if len(media) != 1 {
		t.Errorf("got %d media, want 1 (only image, not PDF or audio)", len(media))
	}
	if len(rest) != 3 {
		t.Errorf("got %d rest, want 3 (text + PDF + audio)", len(rest))
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
	// Use a non-Kimi URL so uploadImageAssets is skipped (gated on
	// api.moonshot.ai). SupportsVision is set from model name "kimi-k3",
	// so the base64 image path is still exercised.
	c := NewClient("", "kimi-k3",
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

func TestVision_KimiMsURLPayload(t *testing.T) {
	c := NewClient("", "kimi-k3",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	if !c.capabilities.SupportsVision {
		t.Fatal("kimi-k3 should have SupportsVision")
	}

	// Build history with an image part. Pre-populate a turnAssets with
	// a file binding to simulate a previously uploaded file.
	imgPart := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}}
	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			imgPart,
			{Text: "describe this"},
		},
	}}

	ta := newTurnAssets()
	ta.bindings[imgPart] = "file-abc123"
	msgs, err := c.toStandardMessages(context.Background(), history, ta)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	msg := msgs[0]

	if _, ok := msg.Content.([]any); !ok {
		t.Fatalf("expected []any content, got %T", msg.Content)
	}

	b, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Verify ms:// URL is used (not base64 data URI)
	if !strings.Contains(string(b), "ms://file-abc123") {
		t.Error("JSON payload missing ms://file-abc123 URL")
	}
	if strings.Contains(string(b), "data:image/png;base64") {
		t.Error("JSON payload should NOT contain base64 data URI when AssetID is set")
	}
}

func TestHydrateMediaAssets(t *testing.T) {
	resolver := &testAssetResolver{
		data: map[string][]byte{
			"asset-1": []byte{0x89, 0x50, 0x4E, 0x47},
		},
	}
	tests := []struct {
		name          string
		parts         []*llm.Part
		resolver      llm.AssetResolver
		visionCap     bool
		wantMIME      string
		wantData      bool
		wantSameSlice bool // true when no mutation expected (copy-on-write)
		wantErr       bool
	}{
		{
			name:      "hydrates AssetID with nil InlineData (test shape)",
			parts:     []*llm.Part{{AssetID: "asset-1"}},
			resolver:  resolver,
			visionCap: true,
			wantData:  true,
		},
		{
			name:      "hydrates reload shape: InlineData present, Data nil — preserves MIMEType",
			parts:     []*llm.Part{{AssetID: "asset-1", InlineData: &llm.Blob{MIMEType: "image/png"}}},
			resolver:  resolver,
			visionCap: true,
			wantMIME:  "image/png",
			wantData:  true,
		},
		{
			name:          "skips part with existing InlineData.Data (already hydrated)",
			parts:         []*llm.Part{{AssetID: "asset-1", InlineData: &llm.Blob{MIMEType: "image/jpeg", Data: []byte{1}}}},
			resolver:      resolver,
			visionCap:     true,
			wantMIME:      "image/jpeg",
			wantData:      true,
			wantSameSlice: true,
		},
		{
			name:          "skips part with no AssetID",
			parts:         []*llm.Part{{Text: "hello"}},
			resolver:      resolver,
			visionCap:     true,
			wantSameSlice: true,
		},
		{
			name:          "nil resolver is no-op",
			parts:         []*llm.Part{{AssetID: "asset-1"}},
			resolver:      nil,
			visionCap:     true,
			wantSameSlice: true,
		},
		{
			name:          "vision-disabled returns input unchanged",
			parts:         []*llm.Part{{AssetID: "asset-1"}},
			resolver:      resolver,
			visionCap:     false,
			wantSameSlice: true,
		},
		{
			name:      "resolve error propagates",
			parts:     []*llm.Part{{AssetID: "missing"}},
			resolver:  resolver,
			visionCap: true,
			wantErr:   true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &client{
				capabilities: llm.Capabilities{SupportsVision: tt.visionCap},
				logger:       &ports.NoOpLogger{},
			}
			got, err := c.hydrateMediaAssets(context.Background(), tt.parts, tt.resolver)
			if (err != nil) != tt.wantErr {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if tt.wantSameSlice && &got[0] != &tt.parts[0] {
				t.Error("expected no copy-on-write: input slice returned unchanged")
			}
			if !tt.wantSameSlice && &got[0] == &tt.parts[0] {
				t.Error("expected copy-on-write: returned slice should be independent")
			}
			if tt.wantData {
				if got[0].InlineData == nil || len(got[0].InlineData.Data) == 0 {
					t.Error("expected InlineData.Data to be populated")
				}
			}
			if tt.wantMIME != "" && got[0].InlineData.MIMEType != tt.wantMIME {
				t.Errorf("MIMEType = %q, want %q", got[0].InlineData.MIMEType, tt.wantMIME)
			}
		})
	}
}

type testAssetResolver struct {
	data map[string][]byte
}

func (r *testAssetResolver) Resolve(ctx context.Context, assetID string) ([]byte, error) {
	d, ok := r.data[assetID]
	if !ok {
		return nil, fmt.Errorf("asset not found: %s", assetID)
	}
	return d, nil
}

func TestPrepareMediaForTurn_ResolverError(t *testing.T) {
	c := NewClient("", "test-model", &auth.BearerAuth{Token: "test"})
	c.capabilities.SupportsVision = true

	history := []*llm.Content{{
		Role:  "user",
		Parts: []*llm.Part{{AssetID: "asset-1"}},
	}}

	// The asset is missing from the resolver's data map, so Resolve fails.
	ta, err := c.prepareMediaForTurn(context.Background(), history, &testAssetResolver{data: map[string][]byte{}})
	if ta != nil {
		t.Errorf("expected nil turnAssets on resolver error, got %v", ta)
	}
	if err == nil {
		t.Error("expected error from resolver failure, got nil")
	}
}

func TestPrepareMediaForTurn_SuccessAppliesParts(t *testing.T) {
	c := NewClient("", "test-model", &auth.BearerAuth{Token: "test"})
	c.capabilities.SupportsVision = true
	c.capabilities.FileUploadMode = llm.FileUploadNone

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("x")}},
		},
	}}

	// The part already carries InlineData.Data, so it is not a hydration
	// candidate — no resolver is required.
	ta, err := c.prepareMediaForTurn(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ta == nil {
		t.Fatal("expected non-nil turnAssets")
	}
	if len(history[0].Parts) != 1 {
		t.Fatalf("expected 1 part preserved, got %d", len(history[0].Parts))
	}
	if history[0].Parts[0].InlineData == nil ||
		history[0].Parts[0].InlineData.MIMEType != "image/png" ||
		string(history[0].Parts[0].InlineData.Data) != "x" {
		t.Errorf("expected InlineData preserved, got %+v", history[0].Parts[0].InlineData)
	}
}

func TestSendChat_MediaPrepError(t *testing.T) {
	c := NewClient("", "test-model", &auth.BearerAuth{Token: "test"})
	c.capabilities.SupportsVision = true

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{AssetID: "asset-1"}}}}

	_, _, err := c.SendChat(context.Background(), history, nil, &testAssetResolver{data: map[string][]byte{}})
	if err == nil {
		t.Fatal("expected media-prep error, got nil")
	}
	if !strings.Contains(err.Error(), "asset not found") {
		t.Errorf("expected 'asset not found' in error, got %q", err.Error())
	}
}

func TestVision_DeepSeekImagePayload(t *testing.T) {
	c := NewClient("https://api.deepseek.com", "deepseek-v4-flash-vision-exp",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	if !c.capabilities.SupportsVision {
		t.Fatal("deepseek-v4-flash-vision-exp should have SupportsVision")
	}
	if c.capabilities.SupportsVideo {
		t.Fatal("deepseek-v4-flash-vision-exp should NOT have SupportsVideo")
	}
	if c.capabilities.FileUploadMode != llm.FileUploadDeepSeek {
		t.Fatalf("expected FileUploadDeepSeek mode, got %d", c.capabilities.FileUploadMode)
	}

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
	b, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(b)
	assertVisionJSONContains(t, s, `"type":"image_url"`, "image_url block")
	assertVisionJSONContains(t, s, "data:image/png;base64", "base64 data URI")
	assertVisionJSONContains(t, s, `"type":"text"`, "text block")
	assertVisionJSONContains(t, s, "describe this", "text content")
	assertVisionJSONAbsent(t, s, `"type":"file"`, "file block")
	assertVisionJSONAbsent(t, s, "file_id", "file_id")
	assertVisionJSONAbsent(t, s, "ms://", "ms:// reference")
}

func TestVision_DeepSeekFileBlockPayload(t *testing.T) {
	c := NewClient("https://api.deepseek.com", "deepseek-v4-flash-vision-exp",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)

	imgPart := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}}
	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			imgPart,
			{Text: "describe this"},
		},
	}}

	ta := newTurnAssets()
	ta.bindings[imgPart] = "file-api-123"

	msgs, err := c.toStandardMessages(context.Background(), history, ta)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	b, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, `"type":"file"`) || !strings.Contains(s, `"file_id":"file-api-123"`) {
		t.Error("JSON payload missing file block with file_id")
	}
	if strings.Contains(s, `"type":"image_url"`) {
		t.Error("JSON payload should NOT contain image_url for a bound DeepSeek part")
	}
	if strings.Contains(s, "ms://") {
		t.Error("JSON payload should NOT contain ms:// references")
	}
	if !strings.Contains(s, `"type":"text"`) || !strings.Contains(s, "describe this") {
		t.Error("JSON payload missing text block")
	}
}

// assertVisionJSONContains fails the test when needle is absent from s.
func assertVisionJSONContains(t *testing.T, s, needle, what string) {
	t.Helper()
	if !strings.Contains(s, needle) {
		t.Errorf("JSON payload missing %s (needle %q)", what, needle)
	}
}

// assertVisionJSONAbsent fails the test when needle is present in s.
func assertVisionJSONAbsent(t *testing.T, s, needle, what string) {
	t.Helper()
	if strings.Contains(s, needle) {
		t.Errorf("JSON payload should NOT contain %s (needle %q)", what, needle)
	}
}

func TestVision_DeepSeekVideoOnly_NoEmptyContent(t *testing.T) {
	c := NewClient("https://api.deepseek.com", "deepseek-v4-flash-vision-exp",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	if !c.capabilities.SupportsVision {
		t.Fatal("deepseek-v4-flash-vision-exp should have SupportsVision")
	}
	if c.capabilities.SupportsVideo {
		t.Fatal("deepseek-v4-flash-vision-exp should NOT have SupportsVideo")
	}

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{0x00, 0x00, 0x00, 0x18}}},
		},
	}}

	msgs, err := c.toStandardMessages(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	b, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	want := `{"role":"user","content":"(video content omitted — this model does not support video input)"}`
	if string(b) != want {
		t.Errorf("exact JSON mismatch:\ngot  %s\nwant %s", b, want)
	}
}

func TestVision_TextOnlyImageOnly_NoEmptyContent(t *testing.T) {
	c := NewClient("https://api.deepseek.com", "deepseek-v4-flash",
		&auth.BearerAuth{Token: "test"},
		WithLogger(&ports.NoOpLogger{}),
	)
	if c.capabilities.SupportsVision {
		t.Fatal("deepseek-v4-flash should NOT have SupportsVision")
	}

	history := []*llm.Content{{
		Role: "user",
		Parts: []*llm.Part{
			{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50, 0x4E, 0x47}}},
		},
	}}

	msgs, err := c.toStandardMessages(context.Background(), history, nil)
	if err != nil {
		t.Fatalf("toStandardMessages failed: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	b, err := json.Marshal(msgs[0])
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	want := `{"role":"user","content":"(image content omitted — this model does not support image input)"}`
	if string(b) != want {
		t.Errorf("exact JSON mismatch:\ngot  %s\nwant %s", b, want)
	}
}
