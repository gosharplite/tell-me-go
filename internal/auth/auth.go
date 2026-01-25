// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package auth handles token management and authentication exclusively for Vertex AI.
package auth

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	GetToken() (string, error)
	Invalidate()
	Apply(req *Request)
}

// Request is a wrapper for the headers needed to apply authentication.
type Request struct {
	Headers map[string]string
}

// VertexAuth handles authentication for Vertex AI using GCP tokens.
type VertexAuth struct {
	Token string
}

func (a *VertexAuth) getCachePath() string {
	return filepath.Join(os.TempDir(), "tell_me_go_token_vertex.txt")
}

// GetToken retrieves the OAuth2 access token with local caching.
func (a *VertexAuth) GetToken() (string, error) {
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
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get gcloud token: %w", err)
	}
	
	token := strings.TrimSpace(string(out))
	
	// 3. Save to cache
	_ = os.WriteFile(cacheFile, []byte(token), 0600)
	
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
	token, _ := a.GetToken()
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
}

