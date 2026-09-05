// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// purposeRecordingLogger implements ports.Logger and captures Warn calls
// so tests can pin the kimiUploadPurpose warning contract byte-for-byte.
type purposeRecordingLogger struct {
	warnMsgs []string
	warnArgs [][]any
}

func (l *purposeRecordingLogger) Error(msg string, args ...any) {}
func (l *purposeRecordingLogger) Warn(msg string, args ...any) {
	l.warnMsgs = append(l.warnMsgs, msg)
	l.warnArgs = append(l.warnArgs, args)
}
func (l *purposeRecordingLogger) Info(msg string, args ...any)  {}
func (l *purposeRecordingLogger) Debug(msg string, args ...any) {}

// Compile-time check that the fake satisfies the logger contract.
var _ ports.Logger = (*purposeRecordingLogger)(nil)

// TestMediaUploadPurpose_PerMode pins the upload-purpose policy per
// FileUploadMode through the dispatcher's public entry point
// (mediaUploadPurpose), covering the uploadability guard, the
// out-of-range mode arm, and each mode-specific helper's decision table.
func TestMediaUploadPurpose_PerMode(t *testing.T) {
	tests := []struct {
		name     string
		mode     llm.FileUploadMode
		part     *llm.Part
		want     string
		wantWarn bool
	}{
		{
			name: "none_mode/nil_inline_data",
			mode: llm.FileUploadNone,
			part: &llm.Part{InlineData: nil},
			want: "",
		},
		{
			name: "none_mode/empty_data",
			mode: llm.FileUploadNone,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{}}},
			want: "",
		},
		{
			name: "none_mode/with_image_data",
			mode: llm.FileUploadNone,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			want: "", // None mode never routes to a helper
		},
		{
			name: "out_of_range_mode",
			mode: llm.FileUploadMode(3), // out-of-range value
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			want: "",
		},
		{
			name: "deepseek/non_image_dropped",
			mode: llm.FileUploadDeepSeek,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}},
			want: "", // images only; video is dropped for vision-only models
		},
		{
			name: "deepseek/small_image_stays_inline",
			mode: llm.FileUploadDeepSeek,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: make([]byte, 1<<20)}},
			want: "", // small images stay inline (not uploaded)
		},
		{
			name: "deepseek/oversized_image_uploads",
			mode: llm.FileUploadDeepSeek,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: make([]byte, maxInlineMediaBytes+1)}},
			want: "user_data",
		},
		{
			name:     "kimi/unsupported_mime_warns",
			mode:     llm.FileUploadKimi,
			part:     &llm.Part{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}},
			want:     "",
			wantWarn: true,
		},
		{
			name: "kimi/video_uploaded",
			mode: llm.FileUploadKimi,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: []byte{0x00, 0x00, 0x00, 0x1C}}},
			want: "video",
		},
		{
			name: "kimi/image_uploaded",
			mode: llm.FileUploadKimi,
			part: &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
			want: "image",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &purposeRecordingLogger{}
			c := &client{
				capabilities: llm.Capabilities{FileUploadMode: tt.mode},
				logger:       rec,
			}

			got := c.mediaUploadPurpose(tt.part)

			if got != tt.want {
				t.Errorf("mediaUploadPurpose() = %q; want %q", got, tt.want)
			}
			if tt.wantWarn {
				if len(rec.warnMsgs) != 1 {
					t.Fatalf("expected exactly 1 Warn call, got %d: %v", len(rec.warnMsgs), rec.warnMsgs)
				}
				if rec.warnMsgs[0] != "skipping_unsupported_media_mime" {
					t.Errorf("warn event = %q; want %q", rec.warnMsgs[0], "skipping_unsupported_media_mime")
				}
			} else if len(rec.warnMsgs) != 0 {
				t.Errorf("expected no Warn calls, got %d: %v", len(rec.warnMsgs), rec.warnMsgs)
			}
		})
	}
}

// TestKimiUploadPurpose_WarningContract pins the Kimi MIME warning contract
// literally: calling through mediaUploadPurpose with a non-media MIME type
// under FileUploadKimi must emit exactly one Warn with event
// "skipping_unsupported_media_mime" and a "mime" attribute carrying the
// rejected MIME type, and must return "" (part skipped).
func TestKimiUploadPurpose_WarningContract(t *testing.T) {
	rec := &purposeRecordingLogger{}
	c := &client{
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadKimi},
		logger:       rec,
	}
	p := &llm.Part{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}}

	got := c.mediaUploadPurpose(p)

	if got != "" {
		t.Errorf("mediaUploadPurpose() = %q; want \"\"", got)
	}
	if len(rec.warnMsgs) != 1 {
		t.Fatalf("expected exactly 1 Warn call, got %d: %v", len(rec.warnMsgs), rec.warnMsgs)
	}
	if rec.warnMsgs[0] != "skipping_unsupported_media_mime" {
		t.Errorf("warn event = %q; want %q", rec.warnMsgs[0], "skipping_unsupported_media_mime")
	}
	args := rec.warnArgs[0]
	found := false
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok || key != "mime" {
			continue
		}
		if args[i+1] != "application/pdf" {
			t.Errorf("warn attr mime = %v; want %q", args[i+1], "application/pdf")
		}
		found = true
	}
	if !found {
		t.Errorf("warn args %v missing key \"mime\" with value \"application/pdf\"", args)
	}
}
