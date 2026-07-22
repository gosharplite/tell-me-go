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
	"strings"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
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

// fileListResponse is the API response from GET /v1/files.
type fileListResponse struct {
	Object string       `json:"object"`
	Data   []fileObject `json:"data"`
}

// fileDeleteResponse is the API response from DELETE /v1/files/{id}.
type fileDeleteResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Deleted bool   `json:"deleted"`
}

// authHeader returns the Authorization header from the client's
// authenticator. Uses the same Apply pattern as createHTTPRequest.
func (c *client) authHeader(ctx context.Context) (string, error) {
	authReq := &auth.Request{Headers: make(map[string]string)}
	if err := c.authenticator.Apply(ctx, authReq); err != nil {
		return "", err
	}
	for _, v := range authReq.Headers {
		return v, nil // return first header value (e.g. "Bearer <token>")
	}
	return "", fmt.Errorf("no auth header produced by authenticator")
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

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/files", &buf)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	authValue, err := c.authHeader(ctx)
	if err != nil {
		return "", fmt.Errorf("get auth header: %w", err)
	}
	req.Header.Set("Authorization", authValue)

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

// listFiles returns all uploaded files for the current user.
func (c *client) listFiles(ctx context.Context) ([]fileObject, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/files", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	authValue, err := c.authHeader(ctx)
	if err != nil {
		return nil, fmt.Errorf("get auth header: %w", err)
	}
	req.Header.Set("Authorization", authValue)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list files failed (status %d)", resp.StatusCode)
	}

	var fl fileListResponse
	if err := json.NewDecoder(resp.Body).Decode(&fl); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	return fl.Data, nil
}

// deleteFile deletes a previously uploaded file by ID.
func (c *client) deleteFile(ctx context.Context, fileID string) error {
	req, err := http.NewRequestWithContext(ctx, "DELETE", c.baseURL+"/files/"+fileID, nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	authValue, err := c.authHeader(ctx)
	if err != nil {
		return fmt.Errorf("get auth header: %w", err)
	}
	req.Header.Set("Authorization", authValue)

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
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/files/"+fileID+"/content", nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	authVal, err := c.authHeader(ctx)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", authVal)

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
// retrieved, and the file is deleted (cleanup). Returns the full
// extracted text content.
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

// uploadedFileIDs returns the file IDs of all parts that have AssetID
// set and whose AssetID looks like a Kimi file ID.
func uploadedFileIDs(parts []*llm.Part) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, p := range parts {
		if p.AssetID != "" && strings.HasPrefix(p.AssetID, "file-") && !seen[p.AssetID] {
			seen[p.AssetID] = true
			ids = append(ids, p.AssetID)
		}
	}
	return ids
}
