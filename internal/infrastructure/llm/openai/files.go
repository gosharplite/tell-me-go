// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

// fileObject is the API response from POST /v1/files.
type fileObject struct {
	ID        string `json:"id"`
	Object    string `json:"object"`
	Bytes     int64  `json:"bytes"`
	CreatedAt int64  `json:"created_at"`
	Filename  string `json:"filename"`
	Purpose   string `json:"purpose"`
	Status    string `json:"status"`
}

// buildFileUploadBody constructs a multipart/form-data body for the
// Kimi file upload API. Returns the body buffer and Content-Type header.
func buildFileUploadBody(data []byte, filename, purpose string) (*bytes.Buffer, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// purpose field
	// multipart.Writer.WriteField cannot fail on a *bytes.Buffer sink.
	// Coverage gap accepted by architect — structurally unreachable.
	if err := w.WriteField("purpose", purpose); err != nil {
		return nil, "", fmt.Errorf("write purpose field: %w", err)
	}

	// file field
	fw, err := w.CreateFormFile("file", filename)
	// multipart.Writer.CreateFormFile cannot fail on a *bytes.Buffer sink.
	// Coverage gap accepted by architect — structurally unreachable.
	if err != nil {
		return nil, "", fmt.Errorf("create form file: %w", err)
	}
	// The form-file writer wraps the *bytes.Buffer; Write cannot fail.
	// Coverage gap accepted by architect — structurally unreachable.
	if _, err := fw.Write(data); err != nil {
		return nil, "", fmt.Errorf("write file data: %w", err)
	}

	// multipart.Writer.Close cannot fail on a *bytes.Buffer sink.
	// Coverage gap accepted by architect — structurally unreachable.
	if err := w.Close(); err != nil {
		return nil, "", fmt.Errorf("close multipart writer: %w", err)
	}

	return &buf, w.FormDataContentType(), nil
}

// isUploadStatusReady returns true when the file upload status is one of
// the accepted values. Live api.moonshot.ai returns "ok" (observed
// 2026-07-23); the docs say "ready". Both are accepted for
// forward-compatibility.
func isUploadStatusReady(status string) bool {
	return status == "ok" || status == "ready"
}

// uploadFile uploads a file to the provider file API and returns the
// file ID. purpose must be "image", "video", or "file-extract".
// filename is the original filename for metadata.
func (c *client) uploadFile(ctx context.Context, data []byte, filename, purpose string) (string, error) {
	buf, contentType, err := buildFileUploadBody(data, filename, purpose)
	if err != nil {
		return "", err
	}

	req, err := c.newAuthenticatedRequest(ctx, "POST", c.baseURL+"/files", buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	return parseFileUploadResponse(resp, c.capabilities.FileUploadMode)
}

// parseFileUploadResponse decodes the JSON file object from the upload
// response and validates it according to the client's FileUploadMode.
// Kimi responses carry a status field ("ok" live, "ready" documented —
// both accepted for forward-compatibility). DeepSeek responses carry
// neither a status field nor a purpose; the object id and object type
// are validated instead. FileUploadNone is defensive: uploadFile must
// not be called in None mode.
func parseFileUploadResponse(resp *http.Response, mode llm.FileUploadMode) (string, error) {
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	var fo fileObject
	if err := json.NewDecoder(resp.Body).Decode(&fo); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}

	switch mode {
	case llm.FileUploadKimi:
		if !isUploadStatusReady(fo.Status) {
			return "", fmt.Errorf("upload status %q (expected ok or ready)", fo.Status)
		}
	case llm.FileUploadDeepSeek:
		if fo.ID == "" {
			return "", fmt.Errorf("upload response missing id")
		}
		if fo.Object != "file" {
			return "", fmt.Errorf("upload response object %q (expected file)", fo.Object)
		}
	case llm.FileUploadNone:
		return "", fmt.Errorf("file upload not supported")
	}

	return fo.ID, nil
}

// deleteFile deletes a previously uploaded file by ID.
func (c *client) deleteFile(ctx context.Context, fileID string) error {
	req, err := c.newAuthenticatedRequest(ctx, "DELETE", c.baseURL+"/files/"+fileID, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete file: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("delete file failed (status %d)", resp.StatusCode)
	}
	return nil
}

// getFileContent retrieves the extracted text content of a file
// uploaded with purpose="file-extract".
func (c *client) getFileContent(ctx context.Context, fileID string) (string, error) {
	req, err := c.newAuthenticatedRequest(ctx, "GET", c.baseURL+"/files/"+fileID+"/content", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("get content: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("get content failed (status %d)", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read content: %w", err)
	}

	return string(body), nil
}

// extractDocument uploads a document to the Kimi file API for text
// extraction and returns the extracted content. The document is
// uploaded with purpose="file-extract", the extracted text is
// retrieved, and the file is deleted (cleanup).
//
// This is infrastructure ready for tool wiring — a future
// kimi-extract-document agent tool would call this via the client
// to extract text from user-provided documents (PDF, DOCX, MD, etc.)
// and inject the result as system-message context.
func (c *client) extractDocument(ctx context.Context, data []byte, filename string) (string, error) {
	fileID, err := c.uploadFile(ctx, data, filename, "file-extract")
	if err != nil {
		return "", fmt.Errorf("upload document: %w", err)
	}
	defer func() {
		// Best-effort cleanup with detached context — not gated by
		// the caller's context deadline.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = c.deleteFile(cleanupCtx, fileID)
	}()

	content, err := c.getFileContent(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("get document content: %w", err)
	}

	return content, nil
}
