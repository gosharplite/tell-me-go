// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
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
