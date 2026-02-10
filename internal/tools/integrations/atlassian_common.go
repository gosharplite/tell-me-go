// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package integrations

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/tools"
)

const defaultAtlassianBaseURL = "https://02007.atlassian.net"

type atlassianProvider struct {
	baseURL   string
	email     string
	token     string
	baseDelay time.Duration
}

func newAtlassianProvider() *atlassianProvider {
	baseURL := os.Getenv("ATLASSIAN_BASE_URL")
	if baseURL == "" {
		baseURL = defaultAtlassianBaseURL
	}
	return &atlassianProvider{
		baseURL:   baseURL,
		email:     os.Getenv("ATLASSIAN_EMAIL"),
		token:     os.Getenv("ATLASSIAN_TOKEN"),
		baseDelay: 1 * time.Second,
	}
}

func (p *atlassianProvider) getAuthHeader() (string, error) {
	if p.email == "" {
		return "", fmt.Errorf("missing ATLASSIAN_EMAIL environment variable")
	}
	if p.token == "" {
		return "", fmt.Errorf("missing ATLASSIAN_TOKEN environment variable")
	}

	auth := fmt.Sprintf("%s:%s", p.email, p.token)
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	return fmt.Sprintf("Basic %s", encoded), nil
}

func (p *atlassianProvider) Do(ctx context.Context, client tools.HTTPClient, req *http.Request) (*http.Response, error) {
	authHeader, err := p.getAuthHeader()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", authHeader)

	var lastResp *http.Response
	baseDelay := p.baseDelay
	if baseDelay == 0 {
		baseDelay = 1 * time.Second
	}

	for i := 0; i <= 3; i++ { // 0 is initial attempt, 1-3 are retries
		// Reset body for retries
		if i > 0 && req.GetBody != nil {
			newBody, err := req.GetBody()
			if err != nil {
				return nil, fmt.Errorf("failed to reset request body: %w", err)
			}
			req.Body = newBody
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// Handle 429
		lastResp = resp
		if i == 3 {
			break // Max retries reached
		}

		// Determine wait time
		wait := baseDelay * (1 << uint(i)) // Exponential: 1s, 2s, 4s
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				wait = time.Duration(seconds) * time.Second
			}
		}

		resp.Body.Close() // Close body of throttled request before waiting

		select {
		case <-time.After(wait):
			// Continue to next attempt
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return lastResp, nil
}
