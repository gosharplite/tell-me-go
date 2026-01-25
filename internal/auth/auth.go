// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package auth handles token management and authentication exclusively for Vertex AI.
package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	GetToken() (string, error)
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

// GetToken retrieves the OAuth2 access token.
func (a *VertexAuth) GetToken() (string, error) {
	if a.Token != "" {
		return a.Token, nil
	}
	// Retrieve token using gcloud
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get gcloud token: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Apply injects the Bearer token into the request headers.
func (a *VertexAuth) Apply(req *Request) {
	token, _ := a.GetToken()
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
}
