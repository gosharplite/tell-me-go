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
	authcontract "github.com/gosharplite/tell-me-go/internal/infrastructure/auth/contract"
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

		// Live api.moonshot.ai returns "ok" (observed 2026-07-23).
		// Docs: https://platform.kimi.ai/docs/api/files-upload.md (example: "ready")
		_ = json.NewEncoder(w).Encode(fileObject{
			ID:     "file-abc123",
			Status: "ok",
		})
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadKimi},
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
		logger:     &ports.NoOpLogger{}, capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadKimi},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err == nil || !strings.Contains(err.Error(), "expected ok or ready") {
		t.Errorf("expected 'expected ok or ready' error, got %v", err)
	}
}

// TestUploadFile_ReadyAlsoAccepted verifies that the documented
// "ready" status is accepted for forward-compatibility, even though
// the live api.moonshot.ai currently returns "ok".
func TestUploadFile_ReadyAlsoAccepted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fileObject{
			ID: "file-ready123",
			// Docs: https://platform.kimi.ai/docs/api/files-upload.md (example: "ready")
			// Server may align with docs in the future — accept both.
			Status: "ready",
		})
	}))
	defer server.Close()

	c := &client{
		baseURL:    strings.TrimSuffix(server.URL, "/"),
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{}, capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadKimi},
	}
	c.authenticator = &fakeAuthenticator{}

	id, err := c.uploadFile(context.Background(), []byte("test"), "doc.md", "file-extract")
	if err != nil {
		t.Fatalf("uploadFile with status 'ready': %v", err)
	}
	if id != "file-ready123" {
		t.Errorf("expected file-ready123, got %s", id)
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
			// Live api.moonshot.ai returns "ok" (observed 2026-07-23).
			// Docs: https://platform.kimi.ai/docs/api/files-upload.md (example: "ready")
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-doc", Status: "ok"})
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
		logger:     &ports.NoOpLogger{}, capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadKimi},
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

// fakeAuthenticator implements authcontract.Authenticator for tests.
type fakeAuthenticator struct{}

func (f *fakeAuthenticator) Apply(ctx context.Context, headers authcontract.AuthHeaders) error {
	headers["Authorization"] = "Bearer test-token"
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

func TestPrepareMediaAssets_Gating(t *testing.T) {
	// no upload capability — should skip
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
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
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

func TestPrepareMediaAssets_KimiURL_Uploads(t *testing.T) {
	var uploaded, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files"):
			uploaded = true
			// Live api.moonshot.ai returns "ok" (observed 2026-07-23).
			// Docs: https://platform.kimi.ai/docs/api/files-upload.md (example: "ready")
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-xyz", Status: "ok"})
		case r.Method == "DELETE":
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}
	c.capabilities = llm.Capabilities{FileUploadMode: llm.FileUploadKimi}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}},
	}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
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

func TestPrepareMediaAssets_KimiURL_UploadsVideo(t *testing.T) {
	var uploaded, deleted bool
	var uploadPurpose string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files"):
			uploaded = true
			if err := r.ParseMultipartForm(10 << 20); err != nil {
				t.Errorf("parse multipart: %v", err)
			}
			uploadPurpose = r.FormValue("purpose")
			// Live api.moonshot.ai returns "ok" (observed 2026-07-23).
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-xyz", Status: "ok"})
		case r.Method == "DELETE":
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}
	c.capabilities = llm.Capabilities{FileUploadMode: llm.FileUploadKimi}

	videoData := []byte{0x00, 0x00, 0x00, 0x1C, 0x66, 0x74, 0x79, 0x70}
	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "video/mp4", Data: videoData}},
	}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uploaded {
		t.Error("expected POST /files upload")
	}
	if uploadPurpose != "video" {
		t.Errorf("expected purpose=video, got %q", uploadPurpose)
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

	// Verify video_url block is produced (not image_url)
	blocks := mediaBlocks(out, ta, llm.Capabilities{SupportsVision: true, SupportsVideo: true, FileUploadMode: llm.FileUploadKimi})
	if len(blocks) != 1 {
		t.Fatalf("expected 1 media block, got %d", len(blocks))
	}
	vb, ok := blocks[0].(videoURLBlock)
	if !ok {
		t.Fatalf("expected videoURLBlock, got %T", blocks[0])
	}
	if vb.Type != "video_url" {
		t.Errorf("expected type=video_url, got %q", vb.Type)
	}
	if !strings.HasPrefix(vb.VideoURL.URL, "ms://") {
		t.Errorf("expected ms:// URL prefix, got %q", vb.VideoURL.URL)
	}

	ta.release(context.Background(), c)
	if !deleted {
		t.Error("expected DELETE after release")
	}
}

func TestPrepareMediaAssets_KimiURL_SkipsUnsupportedMIME(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-should-not-exist", Status: "ok"})
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:    server.URL,
		httpClient: server.Client(),
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &fakeAuthenticator{}
	c.capabilities = llm.Capabilities{FileUploadMode: llm.FileUploadKimi}

	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}},
	}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out
	if uploaded {
		t.Error("PDF should not be uploaded")
	}
	if len(ta.uploaded) != 0 {
		t.Errorf("expected 0 uploaded files, got %d", len(ta.uploaded))
	}
	if len(ta.bindings) != 0 {
		t.Error("PDF should have no bindings")
	}

	ta.release(context.Background(), c)
}

// errAuthenticator implements authcontract.Authenticator and always
// fails, driving newAuthenticatedRequest's Apply error branch.
type errAuthenticator struct{}

func (f *errAuthenticator) Apply(ctx context.Context, headers authcontract.AuthHeaders) error {
	return fmt.Errorf("authenticator failure")
}

func (f *errAuthenticator) Invalidate() {}

func TestUploadFile_AuthenticatorError(t *testing.T) {
	c := &client{baseURL: "https://api.openai.com/v1", httpClient: http.DefaultClient, logger: &ports.NoOpLogger{}}
	c.authenticator = &errAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err == nil {
		t.Fatal("expected authenticator error, got nil")
	}
	if !strings.Contains(err.Error(), "create request") || !strings.Contains(err.Error(), "authenticator failure") {
		t.Errorf("expected 'create request: authenticator failure' in error, got %q", err.Error())
	}
}

func TestUploadFile_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("this is not json"))
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("test"), "cat.png", "image")
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode upload response") {
		t.Errorf("expected 'decode upload response' in error, got %q", err.Error())
	}
}

func TestDeleteFile_AuthenticatorError(t *testing.T) {
	c := &client{baseURL: "https://api.openai.com/v1", httpClient: http.DefaultClient, logger: &ports.NoOpLogger{}}
	c.authenticator = &errAuthenticator{}

	err := c.deleteFile(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected authenticator error, got nil")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("expected 'create request' in error, got %q", err.Error())
	}
}

func TestDeleteFile_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	err := c.deleteFile(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected error status, got nil")
	}
	if !strings.Contains(err.Error(), "delete file failed (status 500)") {
		t.Errorf("expected 'delete file failed (status 500)' in error, got %q", err.Error())
	}
}

func TestGetFileContent_AuthenticatorError(t *testing.T) {
	c := &client{baseURL: "https://api.openai.com/v1", httpClient: http.DefaultClient, logger: &ports.NoOpLogger{}}
	c.authenticator = &errAuthenticator{}

	_, err := c.getFileContent(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected authenticator error, got nil")
	}
	if !strings.Contains(err.Error(), "create request") {
		t.Errorf("expected 'create request' in error, got %q", err.Error())
	}
}

func TestGetFileContent_TransportError(t *testing.T) {
	c := &client{baseURL: "http://uploads.invalid", httpClient: &http.Client{Transport: &customRoundTripper{}}, logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.getFileContent(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
	if !strings.Contains(err.Error(), "get content") {
		t.Errorf("expected 'get content' in error, got %q", err.Error())
	}
}

func TestGetFileContent_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.getFileContent(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected error status, got nil")
	}
	if !strings.Contains(err.Error(), "get content failed (status 500)") {
		t.Errorf("expected 'get content failed (status 500)' in error, got %q", err.Error())
	}
}

func TestGetFileContent_ReadError(t *testing.T) {
	c := &client{baseURL: "http://uploads.invalid", httpClient: &http.Client{Transport: &failingBodyTransport{statusCode: http.StatusOK}}, logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.getFileContent(context.Background(), "file-abc")
	if err == nil {
		t.Fatal("expected read error, got nil")
	}
	if !strings.Contains(err.Error(), "read content") {
		t.Errorf("expected 'read content' in error, got %q", err.Error())
	}
}

func TestExtractDocument_UploadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.extractDocument(context.Background(), []byte("pdf data"), "report.pdf")
	if err == nil {
		t.Fatal("expected upload error, got nil")
	}
	if !strings.Contains(err.Error(), "upload document") {
		t.Errorf("expected 'upload document' in error, got %q", err.Error())
	}
}

func TestExtractDocument_GetContentError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-doc", Status: "ok"})
		case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/content"):
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == "DELETE":
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}, capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadKimi}}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.extractDocument(context.Background(), []byte("pdf data"), "report.pdf")
	if err == nil {
		t.Fatal("expected get-content error, got nil")
	}
	if !strings.Contains(err.Error(), "get document content") {
		t.Errorf("expected 'get document content' in error, got %q", err.Error())
	}
}

func TestSendChat_MediaCleanup(t *testing.T) {
	var deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/files":
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-xyz", Status: "ok"})
		case r.Method == "POST" && r.URL.Path == "/chat/completions":
			w.WriteHeader(http.StatusInternalServerError)
		case r.Method == "DELETE":
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{baseURL: strings.TrimSuffix(server.URL, "/"), httpClient: server.Client(), logger: &ports.NoOpLogger{}, capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadKimi}}
	c.authenticator = &fakeAuthenticator{}

	history := []*llm.Content{{Role: "user", Parts: []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89, 0x50}}}}}}

	_, _, err := c.SendChat(context.Background(), history, nil, nil)
	if err == nil {
		t.Fatal("expected chat error, got nil")
	}
	if !deleted {
		t.Error("expected deferred media cleanup (DELETE) after chat failure")
	}
}

func TestUploadFile_DeepSeekResponse(t *testing.T) {
	var uploadPurpose string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/files" {
			t.Errorf("expected POST /files, got %s %s", r.Method, r.URL.Path)
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		uploadPurpose = r.FormValue("purpose")
		// DeepSeek's upload response contains no status field.
		_ = json.NewEncoder(w).Encode(fileObject{
			ID:     "file-api-abc",
			Object: "file",
		})
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	id, err := c.uploadFile(context.Background(), []byte("large image bytes"), "image.png", "user_data")
	if err != nil {
		t.Fatalf("uploadFile: %v", err)
	}
	if id != "file-api-abc" {
		t.Errorf("expected file-api-abc, got %s", id)
	}
	if uploadPurpose != "user_data" {
		t.Errorf("expected purpose=user_data, got %q", uploadPurpose)
	}
}

func TestPrepareMediaAssets_DeepSeek_SmallImageStaysInline(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-should-not-exist", Object: "file"})
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:      server.URL,
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	data := make([]byte, 1<<20) // 1 MiB — well under the 32 MiB inline cap
	for i := range data {
		data[i] = byte(i % 251)
	}
	parts := []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: data}}}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out
	if uploaded {
		t.Error("small image should NOT be uploaded via POST /files")
	}
	if len(ta.uploaded) != 0 || len(ta.bindings) != 0 {
		t.Error("small image should have no uploads or bindings")
	}
	ta.release(context.Background(), c)
}

func TestPrepareMediaAssets_DeepSeek_OversizedUploads(t *testing.T) {
	var uploaded, deleted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-xyz", Object: "file"})
		}
		if r.Method == "DELETE" {
			deleted = true
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:      server.URL,
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	data := make([]byte, 35<<20) // 35 MiB — over the 32 MiB inline cap, under the 64 MiB upload cap
	for i := range data {
		data[i] = byte(i % 251)
	}
	parts := []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: data}}}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !uploaded {
		t.Error("expected POST /files upload for oversized image")
	}
	if len(ta.uploaded) != 1 {
		t.Errorf("expected 1 uploaded file, got %d", len(ta.uploaded))
	}
	if ta.bindings[out[0]] != "file-api-xyz" {
		t.Error("expected binding for uploaded part")
	}
	ta.release(context.Background(), c)
	if !deleted {
		t.Error("expected DELETE after release")
	}
}

func TestPrepareMediaAssets_DeepSeek_Over64MiB_Errors(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-should-not-exist", Object: "file"})
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:      server.URL,
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	data := make([]byte, (64<<20)+1) // just over the 64 MiB upload cap
	parts := []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: data}}}
	_, _, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err == nil {
		t.Fatal("expected error for image over 64 MiB, got nil")
	}
	if !strings.Contains(err.Error(), "image exceeds 64 MiB upload limit") {
		t.Errorf("expected 'image exceeds 64 MiB upload limit' in error, got %q", err.Error())
	}
	if uploaded {
		t.Error("no upload should be attempted for an over-64MiB image")
	}
}

func TestPrepareMediaAssets_DeepSeek_AggregateBody_Errors(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-should-not-exist", Object: "file"})
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:      server.URL,
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	// Two 20 MiB images stay inline; their base64 sizes sum to ~53 MiB,
	// exceeding the 48 MiB aggregate request-body cap.
	parts := []*llm.Part{
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: make([]byte, 20<<20)}},
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: make([]byte, 20<<20)}},
	}
	_, _, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err == nil {
		t.Fatal("expected error for aggregate inline size over 48 MiB, got nil")
	}
	if !strings.Contains(err.Error(), "aggregate inline image size exceeds 48 MiB limit") {
		t.Errorf("expected 'aggregate inline image size exceeds 48 MiB limit' in error, got %q", err.Error())
	}
	if uploaded {
		t.Error("no upload should be attempted when the aggregate body limit is exceeded")
	}
}

// TestUploadMediaParts_NoneMode_SkipsUploads covers the FileUploadNone
// early return in uploadMediaParts: no upload is attempted (the failing
// transport acts as the detector) and no bindings are recorded.
func TestUploadMediaParts_NoneMode_SkipsUploads(t *testing.T) {
	c := &client{
		baseURL:      "http://uploads.invalid",
		httpClient:   &http.Client{Transport: &customRoundTripper{}},
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadNone},
	}
	c.authenticator = &fakeAuthenticator{}

	parts := []*llm.Part{{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte("x")}}}
	ta := newTurnAssets()
	if err := c.uploadMediaParts(context.Background(), parts, ta); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ta.bindings) != 0 || len(ta.uploaded) != 0 {
		t.Error("None mode should not upload or bind media parts")
	}
}

// TestMediaUploadPurpose_DefaultCase covers the defensive default arm of
// mediaUploadPurpose for an out-of-range FileUploadMode value.
func TestMediaUploadPurpose_DefaultCase(t *testing.T) {
	c := &client{
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadMode(3)}, // out-of-range value
	}
	p := &llm.Part{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{1}}}
	if got := c.mediaUploadPurpose(p); got != "" {
		t.Errorf("expected empty purpose for out-of-range mode, got %q", got)
	}
}

// TestPrepareMediaAssets_DeepSeek_SkipsNilAndNonImageParts covers the
// checkDeepSeekMediaSizes nil/empty and non-image skips, plus the
// mediaUploadPurpose DeepSeek non-image skip: nil-InlineData and
// application/pdf parts must not error and must not be uploaded.
func TestPrepareMediaAssets_DeepSeek_SkipsNilAndNonImageParts(t *testing.T) {
	var uploaded bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/files") {
			uploaded = true
			_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-should-not-exist", Object: "file"})
		}
	}))
	defer server.Close()

	c := &client{
		baseURL:      server.URL,
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{SupportsVision: true, FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	parts := []*llm.Part{
		{InlineData: nil},
		{InlineData: &llm.Blob{MIMEType: "application/pdf", Data: []byte{1}}},
		{InlineData: &llm.Blob{MIMEType: "image/png", Data: []byte{0x89}}}, // tiny image stays inline
	}
	ta, out, err := c.prepareMediaAssets(context.Background(), parts, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = out
	if uploaded {
		t.Error("no upload expected for nil/non-image/tiny-inline parts")
	}
	if len(ta.uploaded) != 0 || len(ta.bindings) != 0 {
		t.Error("expected no uploads or bindings")
	}
	ta.release(context.Background(), c)
}

// TestUploadFile_DeepSeekMissingID covers the DeepSeek missing-id branch
// of parseFileUploadResponse: an upload response without an id fails
// loudly even though the status field is absent.
func TestUploadFile_DeepSeekMissingID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fileObject{Object: "file"}) // no id
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("data"), "image.png", "user_data")
	if err == nil || !strings.Contains(err.Error(), "upload response missing id") {
		t.Errorf("expected 'upload response missing id' error, got %v", err)
	}
}

// TestUploadFile_DeepSeekWrongObject covers the DeepSeek wrong-object
// branch of parseFileUploadResponse: an object other than "file" fails
// loudly.
func TestUploadFile_DeepSeekWrongObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-1", Object: "list"})
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadDeepSeek},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("data"), "image.png", "user_data")
	if err == nil || !strings.Contains(err.Error(), "expected file") {
		t.Errorf("expected 'expected file' object error, got %v", err)
	}
}

// TestUploadFile_NoneMode_Unsupported covers the FileUploadNone defensive
// branch of parseFileUploadResponse: a direct uploadFile call on a
// client without a file-upload mode fails loudly instead of silently
// accepting a response.
func TestUploadFile_NoneMode_Unsupported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(fileObject{ID: "file-api-1", Object: "file"})
	}))
	defer server.Close()

	c := &client{
		baseURL:      strings.TrimSuffix(server.URL, "/"),
		httpClient:   server.Client(),
		logger:       &ports.NoOpLogger{},
		capabilities: llm.Capabilities{FileUploadMode: llm.FileUploadNone},
	}
	c.authenticator = &fakeAuthenticator{}

	_, err := c.uploadFile(context.Background(), []byte("data"), "image.png", "user_data")
	if err == nil || !strings.Contains(err.Error(), "file upload not supported") {
		t.Errorf("expected 'file upload not supported' error, got %v", err)
	}
}
