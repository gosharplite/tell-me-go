// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// errorAuthenticator overrides Apply to return an error for testing.
type errorAuthenticator struct {
	auth.BearerAuth
}

func (e *errorAuthenticator) Apply(ctx context.Context, req *auth.Request) error {
	return fmt.Errorf("mock-apply-error")
}

func TestLLMProviderHealthChecker_Healthy(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, server.URL, nil)

	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != ports.StatusHealthy {
		t.Errorf("expected StatusHealthy, got %s: %s", report.Status, report.Message)
	}

	details := report.Details.(map[string]any)
	if details["provider_name"] != "openai" {
		t.Errorf("expected provider_name openai, got %v", details["provider_name"])
	}
	if _, ok := details["latency_ms"]; !ok {
		t.Error("expected latency_ms in details")
	}
}

func TestLLMProviderHealthChecker_UnhealthyMissingKey(t *testing.T) {
	t.Parallel()
	authMock := &auth.BearerAuth{Token: ""}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://localhost", nil)

	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
	if report.Message != "LLM API key is missing" {
		t.Errorf("unexpected message: %s", report.Message)
	}
}

func TestLLMProviderHealthChecker_UnhealthyApplyError(t *testing.T) {
	t.Parallel()
	authMock := &errorAuthenticator{}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://localhost", nil)

	report, _ := checker.Check(context.Background())
	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
	if !strings.Contains(report.Message, "LLM API key is missing or invalid") {
		t.Errorf("unexpected message: %s", report.Message)
	}
}

func TestLLMProviderHealthChecker_UnhealthyInvalidKey(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "bad-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, server.URL, nil)

	report, _ := checker.Check(context.Background())
	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", report.Status)
	}
	if report.Message != "Invalid API Key or unauthorized access" {
		t.Errorf("unexpected message: %s", report.Message)
	}
}

func TestLLMProviderHealthChecker_DegradedTransient(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, server.URL, nil)

	report, _ := checker.Check(context.Background())
	if report.Status != ports.StatusDegraded {
		t.Errorf("expected StatusDegraded, got %s", report.Status)
	}
}

func TestLLMProviderHealthChecker_DegradedConnectivity(t *testing.T) {
	t.Parallel()
	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://invalid-host-999.local", nil)

	report, _ := checker.Check(context.Background())
	if report.Status != ports.StatusDegraded {
		t.Errorf("expected StatusDegraded, got %s", report.Status)
	}
}

func TestLLMProviderHealthChecker_AnthropicHealthyFallback(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("anthropic", authMock, server.URL, nil)

	report, _ := checker.Check(context.Background())
	if report.Status != ports.StatusHealthy {
		t.Errorf("expected StatusHealthy for Anthropic 404, got %s", report.Status)
	}
}

func TestNewLLMProviderHealthChecker_BaseURLFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		providerName    string
		expectedBaseURL string
	}{
		{
			name:            "openai fallback",
			providerName:    "openai",
			expectedBaseURL: "https://api.openai.com/v1",
		},
		{
			name:            "deepseek fallback",
			providerName:    "deepseek",
			expectedBaseURL: "https://api.openai.com/v1",
		},
		{
			name:            "anthropic fallback",
			providerName:    "anthropic",
			expectedBaseURL: "https://api.anthropic.com/v1",
		},
		{
			name:            "google fallback",
			providerName:    "google",
			expectedBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		},
		{
			name:            "gemini fallback",
			providerName:    "gemini",
			expectedBaseURL: "https://generativelanguage.googleapis.com/v1beta",
		},
		{
			name:            "unknown provider no fallback",
			providerName:    "unknown-provider",
			expectedBaseURL: "",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checker := NewLLMProviderHealthChecker(tt.providerName, nil, "", nil)
			report, err := checker.Check(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if report.Status != ports.StatusUnhealthy {
				t.Errorf("expected StatusUnhealthy, got %s", report.Status)
			}

			if report.Message != "LLM API key is missing (no authenticator)" {
				t.Errorf("unexpected message: %s", report.Message)
			}

			details, ok := report.Details.(map[string]any)
			if !ok {
				t.Fatalf("expected Details to be map[string]any, got %T", report.Details)
			}

			if details["endpoint_url"] != tt.expectedBaseURL {
				t.Errorf("expected endpoint_url %q, got %q", tt.expectedBaseURL, details["endpoint_url"])
			}
		})
	}
}

func TestLLMProviderHealthChecker_BuildRequestError(t *testing.T) {
	t.Parallel()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, "://invalid", nil)

	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
	}
	if !strings.Contains(report.Message, "failed to create health check request") {
		t.Errorf("expected message to contain 'failed to create health check request', got: %s", report.Message)
	}
	if report.Error == nil {
		t.Error("expected report.Error to be non-nil")
	}
}

func TestLLMProviderHealthChecker_NonTransientNonAuthError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, server.URL, nil)

	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
	}
	if report.Message != "LLM provider returned error status: 400" {
		t.Errorf("expected message 'LLM provider returned error status: 400', got: %s", report.Message)
	}
	if report.Error == nil {
		t.Error("expected report.Error to be non-nil")
	}
}

func TestLLMProviderHealthChecker_GeminiEndpointSelection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		providerName string
		baseURL      string
	}{
		{
			name:         "vertex aiplatform url",
			providerName: "gemini",
			baseURL:      "https://us-central1-aiplatform.googleapis.com/v1",
		},
		{
			name:         "custom proxy url",
			providerName: "gemini",
			baseURL:      "https://my-proxy.example.com/v1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			checker := NewLLMProviderHealthChecker(tt.providerName, nil, tt.baseURL, nil)
			report, err := checker.Check(context.Background())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if report.Status != ports.StatusUnhealthy {
				t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
			}

			details, ok := report.Details.(map[string]any)
			if !ok {
				t.Fatalf("expected Details to be map[string]any, got %T", report.Details)
			}

			if details["endpoint_url"] != tt.baseURL {
				t.Errorf("expected endpoint_url %q, got %q", tt.baseURL, details["endpoint_url"])
			}
		})
	}
}

func TestLLMProviderHealthChecker_UnknownProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("unknown-provider", authMock, server.URL, nil)

	report, err := checker.Check(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if report.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s: %s", report.Status, report.Message)
	}
	if !strings.Contains(report.Message, "unknown provider type") {
		t.Errorf("expected message to contain 'unknown provider type', got: %s", report.Message)
	}
}

func TestLLMProviderHealthChecker_NilResponseNilError(t *testing.T) {
	t.Parallel()
	// Call handleHTTPResponse directly with (nil, nil) — this path is reachable
	// if custom middleware or refactored code bypasses the stdlib's RoundTripper guard.
	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://127.0.0.1:1", nil)

	details := make(map[string]any)
	report := &ports.ComponentReport{
		Component: ports.CompLLMProvider,
		Status:    ports.StatusHealthy,
		Message:   "before",
	}

	result := checker.handleHTTPResponse(nil, nil, report, details)
	if result.Status != ports.StatusUnhealthy {
		t.Errorf("expected StatusUnhealthy, got %s", result.Status)
	}
	if !strings.Contains(result.Message, "nil response") {
		t.Errorf("expected message about nil response, got: %s", result.Message)
	}
	if result.Error == nil {
		t.Error("expected report.Error to be non-nil")
	}
}

func TestLLMProviderHealthChecker_ComprehensiveEdgeCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		providerName    string
		baseURL         string
		useServer       bool
		serverHandler   func(w http.ResponseWriter, r *http.Request)
		cancelCtx       bool
		authenticator   auth.Authenticator
		wantStatus      ports.HealthStatus
		wantMsgContains string
	}{
		// Gap 1: Pre-cancelled context — http.NewRequestWithContext succeeds in Go 1.26,
		// but httpClient.Do fails immediately with "context canceled" → connectivity error.
		{
			name:            "pre-cancelled context",
			providerName:    "openai",
			baseURL:         "http://127.0.0.1:1",
			cancelCtx:       true,
			authenticator:   &auth.BearerAuth{Token: "test-key"},
			wantStatus:      ports.StatusDegraded,
			wantMsgContains: "connectivity issue",
		},
		// Gap 2: Google/Gemini without googleapis.com in URL — hits the false branch.
		{
			name:            "gemini without googleapis in URL",
			providerName:    "gemini",
			baseURL:         "http://127.0.0.1:1",
			authenticator:   &auth.APIKeyAuth{APIKey: "test-key"},
			wantStatus:      ports.StatusDegraded,
			wantMsgContains: "connectivity issue",
		},
		// Gap 3: Google/Gemini WITH googleapis.com in URL — hits the true branch.
		{
			name:            "gemini with googleapis in URL",
			providerName:    "google",
			baseURL:         "http://127.0.0.1:1/googleapis.com",
			authenticator:   &auth.APIKeyAuth{APIKey: "test-key"},
			wantStatus:      ports.StatusDegraded,
			wantMsgContains: "connectivity issue",
		},
		// Gap 5: 405 MethodNotAllowed → classifyErrorStatus returns StatusHealthy (same as 404).
		{
			name:         "method not allowed fallback",
			providerName: "openai",
			useServer:    true,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusMethodNotAllowed)
			},
			authenticator:   &auth.BearerAuth{Token: "test-key"},
			wantStatus:      ports.StatusHealthy,
			wantMsgContains: "provider reached",
		},
		// Gap 6a: 502 Bad Gateway → classified as transient → StatusDegraded.
		{
			name:         "bad gateway 502",
			providerName: "openai",
			useServer:    true,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			authenticator:   &auth.BearerAuth{Token: "test-key"},
			wantStatus:      ports.StatusDegraded,
			wantMsgContains: "transient",
		},
		// Gap 6b: 504 Gateway Timeout → classified as transient → StatusDegraded.
		{
			name:         "gateway timeout 504",
			providerName: "openai",
			useServer:    true,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusGatewayTimeout)
			},
			authenticator:   &auth.BearerAuth{Token: "test-key"},
			wantStatus:      ports.StatusDegraded,
			wantMsgContains: "transient",
		},
		// Gap 9: 403 Forbidden → classified as terminal (not auth, not transient) →
		// falls through to classifyErrorStatus → StatusUnhealthy.
		{
			name:         "forbidden 403",
			providerName: "openai",
			useServer:    true,
			serverHandler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusForbidden)
			},
			authenticator:   &auth.BearerAuth{Token: "test-key"},
			wantStatus:      ports.StatusUnhealthy,
			wantMsgContains: "error status: 403",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var baseURL string
			if tt.useServer {
				server := httptest.NewServer(http.HandlerFunc(tt.serverHandler))
				t.Cleanup(func() { server.Close() })
				baseURL = server.URL
			} else {
				baseURL = tt.baseURL
			}

			checker := NewLLMProviderHealthChecker(tt.providerName, tt.authenticator, baseURL, nil)

			ctx := context.Background()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel() // immediately cancel
			}

			report, err := checker.Check(ctx)
			if err != nil {
				t.Fatalf("Check returned unexpected Go error: %v", err)
			}
			if report.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s: %s", tt.wantStatus, report.Status, report.Message)
			}
			if tt.wantMsgContains != "" && !strings.Contains(report.Message, tt.wantMsgContains) {
				t.Errorf("expected message to contain %q, got: %s", tt.wantMsgContains, report.Message)
			}
		})
	}
}

// TestLLMProviderHealthChecker_DefaultTransportFallback verifies the
// defensive fallback at provider_health.go:52. When http.DefaultTransport
// is not a *http.Transport (e.g., replaced by middleware), the constructor
// must gracefully fall back to using http.DefaultTransport directly instead
// of panicking on a failed type assertion.
//
// This test temporarily replaces http.DefaultTransport with a custom
// RoundTripper that is NOT a *http.Transport, which forces the else branch
// of the IIFE. The global is restored via t.Cleanup.
//
// IMPORTANT: This test is NOT parallel-safe because it mutates a global.
// Do NOT add t.Parallel().
func TestLLMProviderHealthChecker_DefaultTransportFallback(t *testing.T) {
	// NOTE: no t.Parallel() — mutates http.DefaultTransport

	// 1. Save and replace http.DefaultTransport with a non-*http.Transport
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })

	customTransport := &customRoundTripper{}
	http.DefaultTransport = customTransport

	// 2. Construct the checker — the IIFE should hit the else branch
	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://127.0.0.1:1", nil)

	// 3. Verify the transport is our custom one (not a clone)
	client := GetHTTPClient(checker)
	if client.Transport != customTransport {
		t.Errorf("expected Transport to be customRoundTripper (fallback path), got %T", client.Transport)
	}
}

// TestLLMProviderHealthChecker_TransportClone verifies the happy path:
// when http.DefaultTransport is a *http.Transport (normal case), the
// constructor clones it instead of reusing the pointer.
func TestLLMProviderHealthChecker_TransportClone(t *testing.T) {
	t.Parallel()

	authMock := &auth.BearerAuth{Token: "test-key"}
	checker := NewLLMProviderHealthChecker("openai", authMock, "http://127.0.0.1:1", nil)

	client := GetHTTPClient(checker)

	// Transport must be non-nil
	if client.Transport == nil {
		t.Fatal("expected Transport to be non-nil")
	}

	// Transport must NOT be the same pointer as http.DefaultTransport
	// (proves Clone() was called, not direct assignment)
	if client.Transport == http.DefaultTransport {
		t.Error("Transport must be a clone of DefaultTransport, not the same pointer")
	}
}

// customRoundTripper is a non-*http.Transport implementation used to
// force the fallback branch in NewLLMProviderHealthChecker's IIFE.
type customRoundTripper struct{}

func (c *customRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("customRoundTripper: not a real transport")
}
