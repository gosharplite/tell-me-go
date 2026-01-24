// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package auth handles token management and authentication for Gemini services.
package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

// Authenticator defines the interface for different authentication methods.
type Authenticator interface {
	GetToken() (string, error)
	Apply(req *Request)
}

// Request is a wrapper for the data needed to apply authentication.
type Request struct {
	QueryParams map[string]string
	Headers     map[string]string
}

// APIKeyAuth handles authentication via a simple API Key.
type APIKeyAuth struct {
	APIKey string
}

func (a *APIKeyAuth) GetToken() (string, error) {
	return a.APIKey, nil
}

func (a *APIKeyAuth) Apply(req *Request) {
	req.QueryParams["key"] = a.APIKey
}

// VertexAuth handles authentication for Vertex AI using GCP tokens.
type VertexAuth struct {
	Token string
}

func (a *VertexAuth) GetToken() (string, error) {
	if a.Token != "" {
		return a.Token, nil
	}
	// Fallback to gcloud if no token provided
	out, err := exec.Command("gcloud", "auth", "print-access-token").Output()
	if err != nil {
		return "", fmt.Errorf("failed to get gcloud token: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *VertexAuth) Apply(req *Request) {
	token, _ := a.GetToken()
	if token != "" {
		req.Headers["Authorization"] = "Bearer " + token
	}
}
