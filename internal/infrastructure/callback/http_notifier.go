// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	domain_callback "github.com/gosharplite/tell-me-go/internal/domain/callback"
)

// HTTPNotifier delivers webhook payloads over HTTP/HTTPS POST.
type HTTPNotifier struct {
	client *http.Client
}

// NewHTTPNotifier constructs an HTTPNotifier with a default 15s timeout client.
func NewHTTPNotifier() *HTTPNotifier {
	return &HTTPNotifier{
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// NewHTTPNotifierWithClient constructs an HTTPNotifier with a custom HTTP client (for tests).
func NewHTTPNotifierWithClient(client *http.Client) *HTTPNotifier {
	if client == nil {
		client = &http.Client{
			Timeout: 15 * time.Second,
		}
	}
	return &HTTPNotifier{
		client: client,
	}
}

// Notify delivers the payload to callbackURL via HTTP POST.
// It injects custom headers, ensures Content-Type is application/json,
// enforces response body closure, and returns an error for any non-2xx status code.
func (n *HTTPNotifier) Notify(ctx context.Context, callbackURL string, headers map[string]string, payload domain_callback.CallbackPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal callback payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, callbackURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create callback request: %w", err)
	}

	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return fmt.Errorf("send callback request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("callback endpoint returned status %d", resp.StatusCode)
	}

	return nil
}
