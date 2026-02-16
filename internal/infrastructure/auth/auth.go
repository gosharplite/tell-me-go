// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package auth handles token management and authentication exclusively for Vertex AI.
package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
)

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	getToken() (string, error)
	Invalidate()
	Apply(req *Request)
}

// Request is a wrapper for the headers needed to apply authentication.
type Request struct {
	Headers map[string]string
}

// VertexAuth handles authentication for Vertex AI using GCP tokens.
type VertexAuth struct {
	Token        string
	tokenCmdFunc func() ([]byte, error)
}

func (a *VertexAuth) getTokenFromGcloud() ([]byte, error) {
	if a.tokenCmdFunc != nil {
		return a.tokenCmdFunc()
	}
	return exec.Command("gcloud", "auth", "print-access-token").Output()
}

func (a *VertexAuth) getCachePath() string {
	uid := os.Getuid()
	uidStr := fmt.Sprintf("%d", uid)
	if uid == -1 {
		// Windows or error, fallback to username
		if user := os.Getenv("USERNAME"); user != "" {
			uidStr = user
		} else if user := os.Getenv("USER"); user != "" {
			uidStr = user
		}
	}
	// Use a user-private subdirectory to prevent predictable filename attacks in /tmp
	dir := filepath.Join(os.TempDir(), "tell-me-go-auth-"+uidStr)
	return filepath.Join(dir, "token.txt")
}

// getToken retrieves the OAuth2 access token with local caching.
func (a *VertexAuth) getToken() (string, error) {
	if a.Token != "" {
		return a.Token, nil
	}

	// 1. Try local cache
	cacheFile := a.getCachePath()
	if info, err := os.Stat(cacheFile); err == nil {
		// Tokens are valid for 1 hour. We use 55 minutes (3300s) as a safe buffer.
		if time.Since(info.ModTime()) < 55*time.Minute {
			content, err := os.ReadFile(cacheFile)
			if err == nil {
				a.Token = strings.TrimSpace(string(content))
				return a.Token, nil
			}
		}
	}

	// 2. Fallback to gcloud
	out, err := a.getTokenFromGcloud()
	if err != nil {
		return "", fmt.Errorf("failed to get gcloud token: %w", err)
	}

	token := strings.TrimSpace(string(out))

	// 3. Save to cache
	cacheDir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(cacheDir, 0700); err == nil {
		_ = storage.AtomicWrite(context.Background(), cacheFile, []byte(token), 0600)
	}

	a.Token = token
	return a.Token, nil
}

// Invalidate clears the current token and deletes the local cache file.
func (a *VertexAuth) Invalidate() {
	a.Token = ""
	_ = os.Remove(a.getCachePath())
}

// Apply injects the Bearer token into the request headers.
func (a *VertexAuth) Apply(req *Request) {
	token, _ := a.getToken()
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
}

// APIKeyAuth handles authentication using a static API key.
type APIKeyAuth struct {
	APIKey string
}

func (a *APIKeyAuth) getToken() (string, error) { return a.APIKey, nil }
func (a *APIKeyAuth) Invalidate()               {}
func (a *APIKeyAuth) Apply(req *Request) {
	if a.APIKey != "" {
		// Default to Gemini-style header; this will be specialized per provider in Phase 2
		req.Headers["x-goog-api-key"] = a.APIKey
	}
}

// BearerAuth handles authentication using a Bearer token.
type BearerAuth struct {
	Token string
}

func (a *BearerAuth) getToken() (string, error) { return a.Token, nil }
func (a *BearerAuth) Invalidate()               {}
func (a *BearerAuth) Apply(req *Request) {
	if a.Token != "" {
		req.Headers["Authorization"] = "Bearer " + a.Token
	}
}

// AnthropicAuth handles authentication using the x-api-key header.
type AnthropicAuth struct {
	APIKey string
}

func (a *AnthropicAuth) getToken() (string, error) { return a.APIKey, nil }
func (a *AnthropicAuth) Invalidate()               {}
func (a *AnthropicAuth) Apply(req *Request) {
	if a.APIKey != "" {
		req.Headers["x-api-key"] = a.APIKey
	}
}
