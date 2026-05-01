// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package atlassian

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

type atlassianProvider struct {
	baseURL   string
	email     string
	token     string
	baseDelay time.Duration
}

func newAtlassianProvider() (*atlassianProvider, error) {
	baseURL := os.Getenv("ATLASSIAN_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("missing required environment variable: ATLASSIAN_BASE_URL")
	}

	return &atlassianProvider{
		baseURL:   baseURL,
		email:     os.Getenv("ATLASSIAN_EMAIL"),
		token:     os.Getenv("ATLASSIAN_TOKEN"),
		baseDelay: 1 * time.Second,
	}, nil
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

	for i := 0; i <= 3; i++ { // 0 is initial attempt, 1-3 are retries
		if i > 0 {
			if err := p.resetRequestBody(req); err != nil {
				return nil, err
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		lastResp = resp
		if i == 3 {
			break
		}

		wait := p.getWaitTime(resp, i)
		_ = resp.Body.Close()

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return lastResp, nil
}

func (p *atlassianProvider) getWaitTime(resp *http.Response, retryCount int) time.Duration {
	baseDelay := p.baseDelay
	if baseDelay == 0 {
		baseDelay = 1 * time.Second
	}

	wait := baseDelay * (1 << uint(retryCount))
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			wait = time.Duration(seconds) * time.Second
		}
	}
	return wait
}

func (p *atlassianProvider) resetRequestBody(req *http.Request) error {
	if req.GetBody != nil {
		newBody, err := req.GetBody()
		if err != nil {
			return fmt.Errorf("failed to reset request body: %w", err)
		}
		req.Body = newBody
	}
	return nil
}
