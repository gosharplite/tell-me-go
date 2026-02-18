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
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/storage"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	getToken(ctx context.Context) (string, error)
	Invalidate()
	Apply(ctx context.Context, req *Request) error
}

// Request is a wrapper for the headers needed to apply authentication.
type Request struct {
	Headers map[string]string
}

// VertexAuth handles authentication for Vertex AI using GCP tokens.
type VertexAuth struct {
	mu           sync.Mutex
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
func (a *VertexAuth) getToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

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
		_ = storage.AtomicWrite(ctx, cacheFile, []byte(token), 0600)
	}

	a.Token = token
	return a.Token, nil
}

// Invalidate clears the current token and deletes the local cache file.
func (a *VertexAuth) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Token = ""
	_ = os.Remove(a.getCachePath())
}

// Apply injects the Bearer token into the request headers.
func (a *VertexAuth) Apply(ctx context.Context, req *Request) error {
	token, err := a.getToken(ctx)
	if err != nil {
		return err
	}
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
	return nil
}

// APIKeyAuth handles authentication using a static API key.
type APIKeyAuth struct {
	APIKey string
}

func (a *APIKeyAuth) getToken(ctx context.Context) (string, error) { return a.APIKey, nil }
func (a *APIKeyAuth) Invalidate()                           {}
func (a *APIKeyAuth) Apply(ctx context.Context, req *Request) error {
	if a.APIKey != "" {
		// Default to Gemini-style header; this will be specialized per provider in Phase 2
		req.Headers["x-goog-api-key"] = a.APIKey
	}
	return nil
}

// BearerAuth handles authentication using a Bearer token.
type BearerAuth struct {
	Token string
}

func (a *BearerAuth) getToken(ctx context.Context) (string, error) { return a.Token, nil }
func (a *BearerAuth) Invalidate()                           {}
func (a *BearerAuth) Apply(ctx context.Context, req *Request) error {
	if a.Token != "" {
		req.Headers["Authorization"] = "Bearer " + a.Token
	}
	return nil
}

// AnthropicAuth handles authentication using the x-api-key header.
type AnthropicAuth struct {
	APIKey string
}

func (a *AnthropicAuth) getToken(ctx context.Context) (string, error) { return a.APIKey, nil }
func (a *AnthropicAuth) Invalidate()                           {}
func (a *AnthropicAuth) Apply(ctx context.Context, req *Request) error {
	if a.APIKey != "" {
		req.Headers["x-api-key"] = a.APIKey
	}
	return nil
}

// ServiceAccountAuth handles authentication using a GCP Service Account JSON file.
// It exchanges the long-lived JSON key for a short-lived (1-hour) OAuth2 access token.
type ServiceAccountAuth struct {
	KeyFilePath     string
	tokenSourceFunc func() (*oauth2.Token, error)
	mu              sync.Mutex
	token           string
	expiry          time.Time
}

// getToken performs the OAuth2 exchange and returns a valid access token.
func (a *ServiceAccountAuth) getToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 1. Check if cached token is still valid (with 5-minute buffer)
	if a.token != "" && time.Now().Add(5*time.Minute).Before(a.expiry) {
		return a.token, nil
	}

	var tok *oauth2.Token
	var err error

	if a.tokenSourceFunc != nil {
		tok, err = a.tokenSourceFunc()
		if err != nil {
			return "", fmt.Errorf("failed to fetch mock oauth2 token: %w", err)
		}
	} else {
		// 2. Read the master secret (key.json) from disk
		data, err := os.ReadFile(a.KeyFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to read service account key: %w", err)
		}

		// 3. Exchange JSON key for a Bearer token via Google OAuth2
		// Scope required for Vertex AI: "https://www.googleapis.com/auth/cloud-platform"
		creds, err := google.CredentialsFromJSON(ctx, data, "https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return "", fmt.Errorf("failed to parse service account JSON: %w", err)
		}

		ts := creds.TokenSource
		tok, err = ts.Token()
		if err != nil {
			return "", fmt.Errorf("failed to fetch oauth2 token: %w", err)
		}
	}

	// 4. Cache the resulting short-lived token
	a.token = tok.AccessToken
	a.expiry = tok.Expiry
	return a.token, nil
}

func (a *ServiceAccountAuth) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = ""
}

func (a *ServiceAccountAuth) Apply(ctx context.Context, req *Request) error {
	token, err := a.getToken(ctx)
	if err != nil {
		return err
	}
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
	return nil
}
