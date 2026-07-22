// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

func TestUploadFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/files" {
			t.Errorf("expected /files, got %s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Errorf("expected multipart, got %s", r.Header.Get("Content-Type"))
		}
		// Verify the file was uploaded
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("purpose") != "image" {
			t.Errorf("expected purpose=image, got %s", r.FormValue("purpose"))
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("form file: %v", err)
		}
		buf := make([]byte, 4)
		_, _ = file.Read(buf)
		if string(buf) != "test" {
			t.Errorf("expected file content 'test', got %q", string(buf))
		}

		_ = json.NewEncoder(w).Encode(fileObject{
			ID:     "file-abc123",
			Status: "ready",
		})
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		capabilities: llm.Capabilities{SupportsVision: true},
		logger:       &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	id, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
	if id != "file-abc123" {
		t.Errorf("expected file-abc123, got %s", id)
	}
}

func TestUploadFile_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error": "unauthorized"}`))
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Errorf("expected unauthorized error, got %v", err)
	}
}

func TestUploadFile_NotReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fileObject{
			ID:     "file-abc123",
			Status: "processing",
		})
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err == nil || !strings.Contains(err.Error(), "expected ready") {
		t.Errorf("expected 'expected ready' error, got %v", err)
	}
}

func TestDeleteFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/files/file-abc") {
			t.Errorf("expected /files/file-abc in path, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	if err := c.deleteFile(context.Background(), "file-abc"); err != nil {
		t.Fatalf("deleteFile: %v", err)
	}
}

func TestGetFileContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("extracted document text"))
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}

	content, err := c.getFileContent(context.Background(), "file-abc")
	if err != nil {
		t.Fatalf("getFileContent: %v", err)
	}
	if content != "extracted document text" {
		t.Errorf("expected 'extracted document text', got %q", content)
	}
}

func TestExtractDocument(t *testing.T) {
	var deletedID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-doc", Status: "ready"})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content"):
			_, _ = w.Write([]byte("doc content here"))
		case r.Method == "DELETE":
			deletedID = strings.TrimPrefix(r.URL.Path, "/files/")
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

	content, err := c.extractDocument(context.Background(), []byte("pdf data"), "report.pdf")
	if err != nil {
		t.Fatalf("extractDocument: %v", err)
	}
	if content != "doc content here" {
		t.Errorf("expected 'doc content here', got %q", content)
	}
	if deletedID != "file-doc" {
		t.Errorf("expected cleanup of file-doc, deleted %q", deletedID)
	}
}

// fakeAuthenticator implements auth.Authenticator for tests.
type fakeAuthenticator struct{}

func (f *fakeAuthenticator) Apply(ctx context.Context, req *auth.Request) error {
	req.Headers["Authorization"] = "Bearer test-token"
	return nil
}

func (f *fakeAuthenticator) Invalidate() {}

func TestCleanupUploadedFiles(t *testing.T) {
	var deletedIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			deletedIDs = append(deletedIDs, strings.TrimPrefix(r.URL.Path, "/files/"))
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

	ta := newTurnAssets()
	ta.uploaded = []string{"file-1", "file-2"}
	ta.release(context.Background(), c)

	if len(deletedIDs) != 2 {
		t.Errorf("expected 2 deletes, got %d: %v", len(deletedIDs), deletedIDs)
	}
}

func TestCollectApplyRoundTrip(t *testing.T) {
	original := []*llm.Content{
		{Role: "system", Parts: []*llm.Part{{Text: "system prompt"}}},
		{Role: "user", Parts: []*llm.Part{{Text: "hello"}, {Text: "world"}}},
		{Role: "model", Parts: []*llm.Part{{Text: "hi"}}},
		{Role: "user", Parts: []*llm.Part{}},
	}
	collected := collectHistoryParts(original)
	// system parts excluded; user: 2 + model: 1 + empty-user: 0 = 3
	if len(collected) != 3 {
		t.Fatalf("expected 3 parts (system excluded), got %d", len(collected))
	}
	// Mutate collected parts
	for i := range collected {
		collected[i] = &llm.Part{Text: fmt.Sprintf("modified-%d", i)}
	}
	applyPreparedParts(original, collected)
	// System untouched
	if original[0].Parts[0].Text != "system prompt" {
		t.Error("system was modified")
	}
	// User parts replaced
	if original[1].Parts[0].Text != "modified-0" {
		t.Error("user part not applied")
	}
	if original[1].Parts[1].Text != "modified-1" {
		t.Error("user part 2 not applied")
	}
	// Model part replaced
	if original[2].Parts[0].Text != "modified-2" {
		t.Error("model part not applied")
	}
	// Empty user still empty
	if len(original[3].Parts) != 0 {
		t.Error("empty user got parts")
	}
}

func TestPrepareImageAssets_Gating(t *testing.T) {
	// Non-Kimi URL — should skip upload entirely
	c := &client{
		baseURL:    "https://api.openai.com/v1",
		httpClient: http.DefaultClient,
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}
	c.capabilities = llm.Capabilities{SupportsVision: true}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}},
	}
	ta, out, err := c.prepareImageAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ta.uploaded) != 0 {
		t.Error("non-Kimi URL should not upload")
	}
	if len(ta.bindings) != 0 {
		t.Error("non-Kimi URL should have no bindings")
	}
	_ = out
	ta.release(context.Background(), c)
}

func TestPrepareImageAssets_KimiURL_Uploads(t *testing.T) {
	var uploaded, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files"):
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-xyz", Status: "ready"})
		case r.Method == "DELETE":
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:    server.URL + "/api.moonshot.ai",
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}
	c.capabilities = llm.Capabilities{SupportsVision: true}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
	}
	ta, out, err := c.prepareImageAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uploaded {
		t.Error("expected POST /files upload")
	}
	if len(ta.uploaded) != 1 {
		t.Errorf("expected 1 uploaded file, got %d", len(ta.uploaded))
	}
	if ta.bindings[out[0]] != "file-xyz" {
		t.Error("expected binding for uploaded part")
	}
	if ta.resolveURL(out[0]) != "ms://file-xyz" {
		t.Errorf("expected ms:// URL, got %s", ta.resolveURL(out[0]))
	}

	ta.release(context.Background(), c)
	if !deleted {
		t.Error("expected DELETE after release")
	}
}
