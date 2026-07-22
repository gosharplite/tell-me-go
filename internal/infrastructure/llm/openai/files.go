// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
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

// fileDeleteResponse is the API response from DELETE /v1/files/{id}.
type fileDeleteResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Deleted bool   `json:"deleted"`
}

// uploadFile uploads a file to the Kimi API and returns the file ID.
// purpose must be "image", "video", or "file-extract".
// filename is the original filename for metadata.
func (c *client) uploadFile(ctx context.Context, data []byte, filename, purpose string) (string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)

	// purpose field
	if err := w.WriteField("purpose", purpose); err != nil {
		return "", fmt.Errorf("write purpose field: %w", err)
	}

	// file field
	fw, err := w.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := fw.Write(data); err != nil {
		return "", fmt.Errorf("write file data: %w", err)
	}

	if err := w.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := c.newAuthenticatedRequest(ctx, "POST", c.baseURL+"/files", &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("upload request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		return "", fmt.Errorf("upload failed (status %d): %s", resp.StatusCode, string(body))
	}

	var fo fileObject
	if err := json.NewDecoder(resp.Body).Decode(&fo); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}

	if fo.Status != "ready" {
		return "", fmt.Errorf("upload status %q (expected ready)", fo.Status)
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
	defer resp.Body.Close()

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
	defer resp.Body.Close()

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
		// Best-effort cleanup; don't shadow the primary error
		_ = c.deleteFile(ctx, fileID)
	}()

	content, err := c.getFileContent(ctx, fileID)
	if err != nil {
		return "", fmt.Errorf("get document content: %w", err)
	}

	return content, nil
}
