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

type AtlassianProvider struct {
	baseURL   string
	Email string
	Token string
	BaseDelay time.Duration
}

func NewAtlassianProvider() (*AtlassianProvider, error) {
	baseURL := os.Getenv("ATLASSIAN_BASE_URL")
	if baseURL == "" {
		return nil, fmt.Errorf("missing required environment variable: ATLASSIAN_BASE_URL")
	}

	return &AtlassianProvider{
		baseURL:   baseURL,
		Email:     os.Getenv("ATLASSIAN_EMAIL"),
		Token:     os.Getenv("ATLASSIAN_TOKEN"),
		BaseDelay: 1 * time.Second,
	}, nil
}

func (p *AtlassianProvider) getAuthHeader() (string, error) {
	if p.Email == "" {
		return "", fmt.Errorf("missing ATLASSIAN_EMAIL environment variable")
	}
	if p.Token == "" {
		return "", fmt.Errorf("missing ATLASSIAN_TOKEN environment variable")
	}

	auth := fmt.Sprintf("%s:%s", p.Email, p.Token)
	encoded := base64.StdEncoding.EncodeToString([]byte(auth))
	return fmt.Sprintf("Basic %s", encoded), nil
}

func (p *AtlassianProvider) Do(ctx context.Context, client tools.HTTPClient, req *http.Request) (*http.Response, error) {
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

		wait := p.GetWaitTime(resp, i)
		_ = resp.Body.Close()

		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	return lastResp, nil
}

func (p *AtlassianProvider) GetWaitTime(resp *http.Response, retryCount int) time.Duration {
	BaseDelay := p.BaseDelay
	if BaseDelay == 0 {
		BaseDelay = 1 * time.Second
	}

	wait := BaseDelay * (1 << uint(retryCount))
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			wait = time.Duration(seconds) * time.Second
		}
	}
	return wait
}

func (p *AtlassianProvider) resetRequestBody(req *http.Request) error {
	if req.GetBody != nil {
		newBody, err := req.GetBody()
		if err != nil {
			return fmt.Errorf("failed to reset request body: %w", err)
		}
		req.Body = newBody
	}
	return nil
}
