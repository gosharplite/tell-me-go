// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package gemini

import (
	"context"
	"errors"
	"net/http"
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

// customRoundTripper is an http.RoundTripper that is NOT an *http.Transport.
// Used by TestInitSDK_TransportCloneFallback to exercise the else branch
// in initSDK's transport initialization.
type customRoundTripper struct{}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       http.NoBody,
		Header:     make(http.Header),
	}, nil
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

func TestInitSDK_TransportFallback(t *testing.T) {
	// Gap 1: Verify that initSDK uses testTransport when set, exercising
	// the injection path that bypasses http.DefaultTransport type assertion.
	// The function is expected to fail at SDK creation (no real endpoint),
	// but the transport selection path is fully exercised.
	sentinel := errors.New("sdk not available in test")

	customTransport := &http.Transport{}
	c := &Client{
		apiURL:        "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models",
		model:         "test-model",
		authenticator: &failingAuthenticator{err: nil},
		testTransport: customTransport,
		newGenaiClient: func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
			// Verify the transport was passed through correctly
			if cfg.HTTPClient.Transport != customTransport {
				t.Errorf("expected customTransport in HTTPClient, got %v", cfg.HTTPClient.Transport)
			}
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
}

// TestInitSDK_TransportCloneFallback closes the [TECHNICAL DEBT] gap: when
// http.DefaultTransport is not an *http.Transport (e.g., it has been replaced
// by a custom RoundTripper), initSDK must fall back to using DefaultTransport
// directly instead of calling Clone(). This test is NOT parallel because it
// mutates the global http.DefaultTransport.
func TestInitSDK_TransportCloneFallback(t *testing.T) {
	// Save and restore DefaultTransport
	orig := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = orig })

	// Replace DefaultTransport with a non-*http.Transport type
	http.DefaultTransport = &customRoundTripper{}

	sentinel := errors.New("transport test complete — SDK init verified")
	transportVerified := false
	c := &Client{
		apiURL:        "https://us-central1-aiplatform.googleapis.com/v1/projects/p/locations/l/publishers/google/models",
		model:         "test-model",
		authenticator: &failingAuthenticator{err: nil}, // auth succeeds
		testTransport: nil,                             // triggers http.DefaultTransport fallback
		newGenaiClient: func(ctx context.Context, cfg *genai.ClientConfig) (*genai.Client, error) {
			// Verify transport was set to our custom round tripper via DefaultTransport
			if cfg.HTTPClient.Transport == http.DefaultTransport {
				transportVerified = true
			}
			return nil, sentinel
		},
	}

	err := c.initSDK(30 * time.Second)
	if err == nil {
		t.Fatal("expected error from mock newGenaiClient, got nil")
	}
	if !errors.Is(err, sentinel) || !strings.Contains(err.Error(), "failed to create genai client") {
		t.Errorf("expected wrapped sentinel, got: %v", err)
	}

	if !transportVerified {
		t.Fatal("transport fallback branch was not exercised; http.DefaultTransport not used")
	}
}
