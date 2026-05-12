// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"google.golang.org/genai"
)

func (c *Client) initSDK(timeout time.Duration) error {
	ctx := context.Background()

	c.mu.RLock()
	apiURL := c.apiURL
	c.mu.RUnlock()

	backend, project, location, baseURL, publisherPath := c.determineBackend(apiURL)
	headers, err := c.prepareAuthHeader(ctx)
	if err != nil {
		return fmt.Errorf("failed to prepare auth headers: %w", err)
	}

	// Merge custom headers from configuration (e.g., for Priority PayGo)
	c.mu.RLock()
	for k, v := range c.headers {
		headers.Set(k, v)
	}
	c.mu.RUnlock()

	var tr http.RoundTripper
	if defaultTr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr = defaultTr.Clone()
	} else {
		tr = http.DefaultTransport
	}

	httpClient := &http.Client{
		Transport: tr,
		Timeout:   timeout,
	}

	clientConfig := &genai.ClientConfig{
		Backend:  backend,
		Project:  project,
		Location: location,
		HTTPOptions: genai.HTTPOptions{
			BaseURL: baseURL,
			Headers: headers,
		},
		HTTPClient: httpClient,
	}

	newClient := c.newGenaiClient
	if newClient == nil {
		newClient = genai.NewClient
	}

	sdkClient, err := newClient(ctx, clientConfig)
	if err != nil {
		return fmt.Errorf("failed to create genai client: %w", err)
	}

	c.mu.Lock()
	c.httpTransport = tr
	c.backend = backend
	c.sdkClient = sdkClient

	// Qualify the model if a publisher path is present
	if publisherPath != "" && !strings.Contains(c.model, "/") {
		c.model = strings.TrimSuffix(publisherPath, "/") + "/" + c.model
	}
	c.mu.Unlock()
	return nil
}

func (c *Client) determineBackend(apiURL string) (genai.Backend, string, string, string, string) {
	var backend genai.Backend
	var project, location, baseURL, publisherPath string

	if strings.Contains(apiURL, "aiplatform.googleapis.com") {
		backend, project, location, baseURL, publisherPath = c.parseVertexAI(apiURL)
	} else {
		backend = genai.BackendGeminiAPI
	}

	// Support for local E2E mocking
	if mockURL := os.Getenv("TELL_ME_MOCK_URL"); mockURL != "" {
		baseURL = mockURL
		if backend == genai.BackendVertexAI {
			if project == "" {
				project = "mock-project"
			}
			if location == "" {
				location = "mock-location"
			}
		}
	}

	return backend, project, location, baseURL, publisherPath
}

func (c *Client) parseVertexAI(apiURL string) (genai.Backend, string, string, string, string) {
	parts := strings.Split(apiURL, "/")
	project := findInParts(parts, "projects")
	location := findInParts(parts, "locations")

	// Extract publisher path segment appearing after /locations/{location}/
	var publisherPath string
	locationKey := "/locations/" + location + "/"
	if idx := strings.Index(apiURL, locationKey); idx != -1 {
		pathSegment := apiURL[idx+len(locationKey):]
		// The segment should end before /v1/ or any query params if they existed,
		// but typically Vertex AI URLs are project/location focused.
		if v1Idx := strings.Index(pathSegment, "/v1/"); v1Idx != -1 {
			publisherPath = pathSegment[:v1Idx]
		} else {
			publisherPath = pathSegment
		}
	}

	baseURL := ""
	if idx := strings.Index(apiURL, "/v1/"); idx != -1 {
		baseURL = apiURL[:idx+1]
	}
	return genai.BackendVertexAI, project, location, baseURL, publisherPath
}

func findInParts(parts []string, key string) string {
	for i, p := range parts {
		if p == key && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

func (c *Client) prepareAuthHeader(ctx context.Context) (http.Header, error) {
	authReq := &auth.Request{
		Headers: make(map[string]string),
	}
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()

	if err := authenticator.Apply(ctx, authReq); err != nil {
		return nil, err
	}

	headers := make(http.Header)
	for k, v := range authReq.Headers {
		headers.Set(k, v)
	}
	return headers, nil
}

// RefreshAuth invalidates the current token and re-initializes the SDK client.
func (c *Client) RefreshAuth() error {
	c.mu.RLock()
	authenticator := c.authenticator
	c.mu.RUnlock()
	authenticator.Invalidate()
	// Using a default 1m timeout for RefreshAuth to prevent hangs
	return c.initSDK(1 * time.Minute)
}

type idleConnectionCloser interface {
	CloseIdleConnections()
}

// ResetConnections clears the underlying connection pool.
func (c *Client) ResetConnections() {
	c.mu.RLock()
	tr := c.httpTransport
	c.mu.RUnlock()

	if closer, ok := tr.(idleConnectionCloser); ok {
		closer.CloseIdleConnections()
	}
}
