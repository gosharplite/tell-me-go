// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package auth handles token management and authentication exclusively for Vertex AI.
//
// Coverage note: APIKeyAuth.Invalidate, BearerAuth.Invalidate, AnthropicAuth.Invalidate,
// and noOpAuth.Invalidate are intentionally no-op bodies. They are called by production
// code via the Authenticator interface and exercised by TestOtherAuthenticators,
// TestNoOpAuth, and TestAuthInvalidate_Additional, but coverage instrumentation does
// not count empty/no-op method bodies as covered. These are not error-handling gaps.
package auth

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/persistence"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
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
	// CacheDir allows overriding the default cache location. Primarily for testing.
	CacheDir string
}

// NewVertexAuth returns a VertexAuth wired with the production gcloud executor
// as its default token source. Tests may construct VertexAuth directly with a
// custom tokenCmdFunc to bypass gcloud.
func NewVertexAuth() *VertexAuth {
	return &VertexAuth{
		tokenCmdFunc: func() ([]byte, error) {
			return execCommand("gcloud", "auth", "print-access-token").Output()
		},
	}
}

func (a *VertexAuth) getTokenFromGcloud() ([]byte, error) {
	if a.tokenCmdFunc == nil {
		return nil, fmt.Errorf("tokenCmdFunc not wired; use NewVertexAuth() to construct VertexAuth")
	}
	return a.tokenCmdFunc()
}

var getUID = os.Getuid

// execCommand is a package-level variable to allow tests to inject
// command-execution failures. Defaults to exec.Command.
var execCommand = exec.Command

func (a *VertexAuth) getCachePath() string {
	if a.CacheDir != "" {
		return filepath.Join(a.CacheDir, "token.txt")
	}
	uid := getUID()
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

// readCacheFile attempts to read a valid cached token from disk.
// Returns ("", false) when the cache file is missing, expired
// (older than 55 minutes), or unreadable.
func (a *VertexAuth) readCacheFile() (string, bool) {
	cacheFile := a.getCachePath()
	info, err := os.Stat(cacheFile)
	if err != nil {
		return "", false
	}
	// Tokens are valid for 1 hour. We use 55 minutes (3300s) as a safe buffer.
	if time.Since(info.ModTime()) >= 55*time.Minute {
		return "", false
	}
	content, err := os.ReadFile(cacheFile)
	if err != nil {
		log.Printf("failed to read auth cache file %s: %v", cacheFile, err)
		return "", false
	}
	return strings.TrimSpace(string(content)), true
}

// writeCacheFile persists the token to the local cache directory.
// Failures are logged but not returned — cache writes are best-effort.
func (a *VertexAuth) writeCacheFile(ctx context.Context, token string) {
	cacheFile := a.getCachePath()
	cacheDir := filepath.Dir(cacheFile)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		log.Printf("failed to create auth cache directory: %v", err)
		return
	}
	if err := persistence.AtomicWrite(ctx, &persistence.OSFileSystem{}, cacheFile, []byte(token), 0600); err != nil {
		log.Printf("failed to write auth cache: %v", err)
	}
}

// getToken retrieves the OAuth2 access token with local caching.
func (a *VertexAuth) getToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Token != "" {
		return a.Token, nil
	}

	// 1. Try local cache
	if token, ok := a.readCacheFile(); ok {
		a.Token = token
		return a.Token, nil
	}

	// 2. Fallback to gcloud
	out, err := a.getTokenFromGcloud()
	if err != nil {
		return "", fmt.Errorf("failed to get gcloud token: %w", err)
	}

	token := strings.TrimSpace(string(out))

	// 3. Save to cache
	a.writeCacheFile(ctx, token)

	a.Token = token
	return a.Token, nil
}

// Invalidate clears the current token and deletes the local cache file.
func (a *VertexAuth) Invalidate() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Token = ""
	if err := os.Remove(a.getCachePath()); err != nil && !os.IsNotExist(err) {
		log.Printf("failed to remove auth cache file: %v", err)
	}
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

func (a *APIKeyAuth) Invalidate() {
}
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

func (a *BearerAuth) Invalidate() {
}
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

func (a *AnthropicAuth) Invalidate() {
}
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

// checkCachedToken returns the cached token if it is still valid.
// A token is considered valid if it is non-empty and has more than
// 5 minutes remaining before expiry.
// Caller MUST hold a.mu.
func (a *ServiceAccountAuth) checkCachedToken() (string, bool) {
	if a.token != "" && time.Now().Add(5*time.Minute).Before(a.expiry) {
		return a.token, true
	}
	return "", false
}

// fetchGoogleToken obtains an OAuth2 token either via the injected
// tokenSourceFunc (test path) or by reading the service account JSON
// key from disk and exchanging it with Google's OAuth2 endpoint.
// Caller MUST hold a.mu.
func (a *ServiceAccountAuth) fetchGoogleToken(ctx context.Context) (*oauth2.Token, error) {
	if a.tokenSourceFunc != nil {
		tok, err := a.tokenSourceFunc()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch mock oauth2 token: %w", err)
		}
		return tok, nil
	}

	if a.KeyFilePath == "" {
		return nil, fmt.Errorf("failed to read service account key: KeyFilePath is empty")
	}
	data, err := os.ReadFile(a.KeyFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read service account key: %w", err)
	}

	creds, err := google.CredentialsFromJSONWithType(ctx, data, google.ServiceAccount, "https://www.googleapis.com/auth/cloud-platform")
	if err != nil {
		return nil, fmt.Errorf("failed to parse service account JSON: %w", err)
	}

	ts := creds.TokenSource
	tok, err := ts.Token()
	if err != nil {
		return nil, fmt.Errorf("failed to fetch oauth2 token: %w", err)
	}
	return tok, nil
}

// getToken performs the OAuth2 exchange and returns a valid access token.
func (a *ServiceAccountAuth) getToken(ctx context.Context) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if token, ok := a.checkCachedToken(); ok {
		return token, nil
	}

	tok, err := a.fetchGoogleToken(ctx)
	if err != nil {
		return "", err
	}

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

// noOpAuth implements the Authenticator interface for providers that do not require authentication.
type noOpAuth struct{}

func (a *noOpAuth) Invalidate() {
}
func (a *noOpAuth) Apply(ctx context.Context, req *Request) error {
	return nil
}
