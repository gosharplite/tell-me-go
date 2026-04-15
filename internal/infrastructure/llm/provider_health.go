// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/llm/llmerr"
)

// LLMProviderHealthChecker implements ports.HealthChecker for LLM providers.
type LLMProviderHealthChecker struct {
	providerName  string
	authenticator auth.Authenticator
	baseURL       string
	httpClient    *http.Client
	gateway       llm.LLMGateway
}

// NewLLMProviderHealthChecker creates a new LLMProviderHealthChecker.
func NewLLMProviderHealthChecker(providerName string, authenticator auth.Authenticator, baseURL string, gateway llm.LLMGateway) *LLMProviderHealthChecker {
	if baseURL == "" {
		switch strings.ToLower(providerName) {
		case "openai", "deepseek":
			baseURL = "https://api.openai.com/v1"
		case "anthropic":
			baseURL = "https://api.anthropic.com/v1"
		case "google", "gemini":
			baseURL = "https://generativelanguage.googleapis.com/v1beta"
		}
	}

	return &LLMProviderHealthChecker{
		providerName:  providerName,
		authenticator: authenticator,
		baseURL:       strings.TrimSuffix(baseURL, "/"),
		httpClient:    &http.Client{Timeout: 5 * time.Second},
		gateway:       gateway,
	}
}

// Check performs a diagnostic check on the LLM provider.
func (c *LLMProviderHealthChecker) Check(ctx context.Context) (*ports.ComponentReport, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	details := make(map[string]any)
	details["provider_name"] = c.providerName
	details["endpoint_url"] = c.baseURL

	report := &ports.ComponentReport{
		Component: ports.CompLLMProvider,
		Status:    ports.StatusHealthy,
		Message:   fmt.Sprintf("%s provider is healthy", c.providerName),
		Details:   details,
	}

	// Step A: Configuration Check
	if c.authenticator == nil {
		report.Status = ports.StatusUnhealthy
		report.Message = "LLM API key is missing (no authenticator)"
		return report, nil
	}

	authReq := &auth.Request{Headers: make(map[string]string)}
	if err := c.authenticator.Apply(ctx, authReq); err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = "LLM API key is missing or invalid"
		report.Error = err
		return report, nil
	}
	if len(authReq.Headers) == 0 {
		report.Status = ports.StatusUnhealthy
		report.Message = "LLM API key is missing"
		return report, nil
	}

	// Step B: Connectivity Check
	method, url := c.getPingEndpoint()
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		report.Status = ports.StatusUnhealthy
		report.Message = fmt.Sprintf("failed to create health check request: %v", err)
		report.Error = err
		return report, nil
	}

	// Apply authentication headers
	for k, v := range authReq.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	latency := time.Since(start)
	details["latency_ms"] = latency.Milliseconds()

	if err != nil {
		// Step D: Transient Failures (DNS, Timeout, etc.)
		report.Status = ports.StatusDegraded
		report.Message = fmt.Sprintf("connectivity issue: %v", err)
		report.Error = err
		return report, nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		// Step C: Auth Check and other API errors
		classified := llmerr.Classify(&llmerr.APIError{Status: resp.StatusCode})

		if errors.Is(classified, llm.ErrAuth) {
			report.Status = ports.StatusUnhealthy
			report.Message = "Invalid API Key or unauthorized access"
		} else if llm.IsTransient(classified) {
			report.Status = ports.StatusDegraded
			report.Message = "LLM provider is experiencing transient issues"
		} else {
			// For some endpoints, 404 or 405 might just mean the ping path is wrong but the server is up.
			// However, for OpenAI/Gemini /models, we expect 200.
			// For Anthropic, we might get 404 on the base URL.
			if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
				report.Status = ports.StatusHealthy
				report.Message = fmt.Sprintf("%s provider reached (ping returned %d)", c.providerName, resp.StatusCode)
			} else {
				report.Status = ports.StatusUnhealthy
				report.Message = fmt.Sprintf("LLM provider returned error status: %d", resp.StatusCode)
			}
		}
		report.Error = classified
		return report, nil
	}

	return report, nil
}

func (c *LLMProviderHealthChecker) getPingEndpoint() (string, string) {
	p := strings.ToLower(c.providerName)
	switch p {
	case "openai", "deepseek":
		return "GET", c.baseURL + "/models"
	case "google", "gemini":
		// Gemini API often uses a different base for models or needs the key as a query param.
		// But if we use the baseURL passed in (which usually includes the key or uses headers),
		// we try a standard models list.
		if strings.Contains(c.baseURL, "googleapis.com") {
			return "GET", c.baseURL + "/models"
		}
		return "GET", c.baseURL + "/models"
	case "anthropic":
		// Anthropic doesn't have a standard GET /v1/models that works reliably for a ping.
		// We use the base URL to verify connectivity.
		return "GET", c.baseURL
	default:
		return "GET", c.baseURL
	}
}
