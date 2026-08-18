// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/pkg/testfixtures"
)

// This file closes edge-case branches in client.go with white-box
// (package openai) tests: fileExtensionFromMIME, injectPersona,
// uploadMediaParts, prepareMediaAssets, turnAssets.release, and the
// exported ExtractDocument wrapper.

// TestFileExtensionFromMIME covers the empty-MIME default and the
// malformed (no-slash) fallback of fileExtensionFromMIME, plus the
// two happy-path parses used by uploadMediaParts.
func TestFileExtensionFromMIME(t *testing.T) {
	tests := []struct {
		name string
		mime string
		want string
	}{
		{"empty mime defaults to png", "", "png"},
		{"malformed mime without slash defaults to png", "octet-stream", "png"},
		{"image png", "image/png", "png"},
		{"video mp4", "video/mp4", "mp4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := fileExtensionFromMIME(tt.mime); got != tt.want {
				t.Errorf("fileExtensionFromMIME(%q) = %q, want %q", tt.mime, got, tt.want)
			}
		})
	}
}

// recordingSink implements openaiSink for white-box tests, recording
// every AddMessage/AddToolResponse call for assertions.
type recordingSink struct {
	messages      []recordedMessage
	toolResponses []recordedToolResponse
}

type recordedMessage struct {
	role      string
	content   any
	reasoning *string
	toolCalls []toolCall
}

type recordedToolResponse struct {
	id       string
	response string
}

func (r *recordingSink) AddMessage(role string, content any, reasoning *string, toolCalls []toolCall) {
	r.messages = append(r.messages, recordedMessage{role: role, content: content, reasoning: reasoning, toolCalls: toolCalls})
}

func (r *recordingSink) AddToolResponse(id, response string) {
	r.toolResponses = append(r.toolResponses, recordedToolResponse{id: id, response: response})
}

// TestInjectPersona_DeveloperRole covers the UseDeveloperRole branch of
// injectPersona: the first call for a user message must emit a
// "developer" message and flip the injected flag; a second call with the
// flag already set must not emit another message.
func TestInjectPersona_DeveloperRole(t *testing.T) {
	c := &client{
		persona:      "p",
		capabilities: llm.Capabilities{UseDeveloperRole: true},
	}
	sink := &recordingSink{}
	injected := false

	c.injectPersona(sink, &injected, "user")

	if !injected {
		t.Fatal("expected personaInjected to become true")
	}
	if len(sink.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sink.messages))
	}
	if sink.messages[0].role != "developer" {
		t.Errorf("expected role 'developer', got %q", sink.messages[0].role)
	}
	if sink.messages[0].content != "p" {
		t.Errorf("expected persona content 'p', got %v", sink.messages[0].content)
	}
	if sink.messages[0].reasoning != nil || sink.messages[0].toolCalls != nil {
		t.Errorf("expected nil reasoning and toolCalls, got %v / %v", sink.messages[0].reasoning, sink.messages[0].toolCalls)
	}

	// Negative case: already injected → no second message.
	c.injectPersona(sink, &injected, "user")
	if len(sink.messages) != 1 {
		t.Errorf("expected no second message, got %d messages", len(sink.messages))
	}
}

// TestUploadMediaParts_SkipsNilAndEmptyData covers the nil/empty
// InlineData skip in uploadMediaParts. A transport that fails on any
// request acts as the detector: had an upload been attempted, the test
// would fail with an unexpected error.
func TestUploadMediaParts_SkipsNilAndEmptyData(t *testing.T) {
	c := &client{
		baseURL:    "http://uploads.invalid",
		httpClient: &http.Client{Transport: &customRoundTripper{}},
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	parts := []*llm.Part{
		{InlineData: nil},
		{InlineData: &llm.Blob{MIMEType: "image/png"}},
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{}}},
	}
	ta := newTurnAssets()

	if err := c.uploadMediaParts(context.Background(), parts, ta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ta.bindings) != 0 {
		t.Errorf("expected no bindings, got %d", len(ta.bindings))
	}
	if len(ta.uploaded) != 0 {
		t.Errorf("expected no uploaded files, got %d", len(ta.uploaded))
	}
}

// TestUploadMediaParts_SkipsAlreadyUploaded covers the already-uploaded
// binding skip in uploadMediaParts: a pre-populated binding must prevent
// a second upload (detected via the failing transport).
func TestUploadMediaParts_SkipsAlreadyUploaded(t *testing.T) {
	c := &client{
		baseURL:    "http://uploads.invalid",
		httpClient: &http.Client{Transport: &customRoundTripper{}},
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	p := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("x")}}
	ta := newTurnAssets()
	ta.bindings[p] = "ms://file1"

	if err := c.uploadMediaParts(context.Background(), []*llm.Part{p}, ta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ta.bindings[p] != "ms://file1" {
		t.Errorf("expected binding preserved, got %q", ta.bindings[p])
	}
	if len(ta.bindings) != 1 {
		t.Errorf("expected exactly 1 binding, got %d", len(ta.bindings))
	}
	if len(ta.uploaded) != 0 {
		t.Errorf("expected no new uploads, got %d", len(ta.uploaded))
	}
}

// TestUploadMediaParts_UploadFailure covers the upload-failure path of
// uploadMediaParts: a failing transport must surface an error wrapping
// "upload media" and leave no bindings or uploaded files behind.
func TestUploadMediaParts_UploadFailure(t *testing.T) {
	c := &client{
		baseURL:    "http://uploads.invalid",
		httpClient: &http.Client{Transport: &customRoundTripper{}},
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("x")}},
	}
	ta := newTurnAssets()

	err := c.uploadMediaParts(context.Background(), parts, ta)
	if err == nil {
		t.Fatal("expected error from failing upload, got nil")
	}
	if !strings.Contains(err.Error(), "upload media") {
		t.Errorf("expected error to contain 'upload media', got %q", err.Error())
	}
	if len(ta.bindings) != 0 {
		t.Errorf("expected no bindings after failure, got %d", len(ta.bindings))
	}
	if len(ta.uploaded) != 0 {
		t.Errorf("expected no uploaded files after failure, got %d", len(ta.uploaded))
	}
}

// TestPrepareMediaAssets_UploadErrorPropagates covers the
// uploadMediaParts error propagation path in prepareMediaAssets: with
// SupportsFileUpload and a failing upload, prepareMediaAssets must
// return a non-nil error.
func TestPrepareMediaAssets_UploadErrorPropagates(t *testing.T) {
	c := &client{
		baseURL:      "http://uploads.invalid",
		httpClient:   &http.Client{Transport: &customRoundTripper{}},
		capabilities: llm.Capabilities{SupportsFileUpload: true},
		logger:       &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("x")}},
	}

	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err == nil {
		t.Fatal("expected error from failing upload, got nil")
	}
	if !strings.Contains(err.Error(), "upload media") {
		t.Errorf("expected 'upload media' in error, got %q", err.Error())
	}
	if ta == nil {
		t.Fatal("expected non-nil turnAssets")
	}
	if len(ta.uploaded) != 0 {
		t.Errorf("expected no uploaded files, got %d", len(ta.uploaded))
	}
	_ = out
}

// TestTurnAssetsRelease_DeleteFailureLogsWarning covers the
// deleteFile-failure branch of turnAssets.release: when cleanup fails,
// the failure must be logged (best-effort) rather than propagated.
func TestTurnAssetsRelease_DeleteFailureLogsWarning(t *testing.T) {
	spy := &testfixtures.SpyLogger{}
	c := &client{
		baseURL:    "http://uploads.invalid",
		httpClient: &http.Client{Transport: &customRoundTripper{}},
		logger:     spy,
	}
	c.authenticator = &fakeAuthenticator{}

	ta := newTurnAssets()
	ta.uploaded = []string{"file-1"}

	ta.release(context.Background(), c)

	if !spy.CalledWith("Warn", "cleanup_uploaded_file_failed") {
		t.Error("expected Warn cleanup_uploaded_file_failed when deleteFile fails")
	}
}

// TestExtractDocument_ExportedWrapper covers the exported ExtractDocument
// wrapper, which delegates to the unexported extractDocument pipeline
// (upload → get content → delete).
func TestExtractDocument_ExportedWrapper(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-doc", Status: "ok"})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content"):
			_, _ = w.Write([]byte("extracted wrapper text"))
		case r.Method == "DELETE":
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	content, err := c.ExtractDocument(context.Background(), []byte("pdf data"), "report.pdf")
	if err != nil {
		t.Fatalf("ExtractDocument: %v", err)
	}
	if content != "extracted wrapper text" {
		t.Errorf("expected 'extracted wrapper text', got %q", content)
	}
	if !deleted {
		t.Error("expected DELETE cleanup after extraction")
	}
}

func TestResponsesSink_AddMessage_NonStringContent(t *testing.T) {
	spy := &testfixtures.SpyLogger{}
	sink := &responsesSink{client: &client{logger: spy, model: "test-model"}}

	sink.AddMessage("user", []any{map[string]any{"type": "image_url"}}, nil, nil)

	if !spy.CalledWith("Warn", "responses_sink_non_string_content") {
		t.Error("expected Warn responses_sink_non_string_content for non-string content")
	}
	if len(sink.items) != 0 {
		t.Errorf("expected no items appended for non-string content, got %d", len(sink.items))
	}
}
