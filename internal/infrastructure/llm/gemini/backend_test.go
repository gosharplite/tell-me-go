// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"google.golang.org/genai"
)

// failingAuthenticator is a test double that always fails Apply.
type failingAuthenticator struct {
	err error
}

func (f *failingAuthenticator) Invalidate() {}

func (f *failingAuthenticator) Apply(_ context.Context, _ *auth.Request) error {
	return f.err
}

func TestInitSDK_PrepareAuthHeaderError(t *testing.T) {
	sentinel := errors.New("stub auth failure")

	c := &Client{
		apiURL:        "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models",
		model:         "test-model",
		authenticator: &failingAuthenticator{err: sentinel},
		headers:       nil,
	}

	err := c.initSDK(30 * time.Second)

	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to prepare auth headers") {
		t.Errorf("expected error to contain %q, got %q", "failed to prepare auth headers", err.Error())
	}

	if !errors.Is(err, sentinel) {
		t.Errorf("expected error to wrap sentinel %q, got %v", sentinel.Error(), err)
	}
}

func TestInitSDK_GenaiNewClientError(t *testing.T) {
	sentinel := errors.New("sdk init explosion")

	c := &Client{
		apiURL:        "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models",
		model:         "test-model",
		authenticator: &failingAuthenticator{err: nil}, // succeeds: nil error
		headers:       nil,
		newGenaiClient: func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
			return nil, sentinel
		},
	}

	err := c.initSDK(30 * time.Second)

	if err == nil {
		t.Fatal("expected non-nil error, got nil")
	}

	if !strings.Contains(err.Error(), "failed to create genai client") {
		t.Errorf("expected error to contain %q, got %q", "failed to create genai client", err.Error())
	}

	if !errors.Is(err, sentinel) {
		t.Errorf("expected error to wrap sentinel %q, got %v", sentinel.Error(), err)
	}
}