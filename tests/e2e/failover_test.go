// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gosharplite/tell-me-go/internal/domain/llm"
	"github.com/gosharplite/tell-me-go/internal/domain/tools"
	infra_llm "github.com/gosharplite/tell-me-go/internal/infrastructure/llm"
)

// primaryMockClient returns a transient error to trigger failover.
type primaryMockClient struct{ callCount *int32 }

func (m *primaryMockClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	atomic.AddInt32(m.callCount, 1)
	return nil, nil, fmt.Errorf("%w: HTTP 503 Service Unavailable", llm.ErrTransient)
}

func (m *primaryMockClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *primaryMockClient) GenerateImages(ctx context.Context, model, prompt, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *primaryMockClient) RefreshAuth() error { return nil }

// secondaryMockClient returns a success response.
type secondaryMockClient struct{ callCount *int32 }

func (m *secondaryMockClient) Generate(ctx context.Context, input []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	atomic.AddInt32(m.callCount, 1)
	return &llm.Content{
		Role:  "model",
		Parts: []*llm.Part{{Text: "Failover successful — response from secondary!"}},
	}, &llm.Metrics{PromptTokens: 5, ResponseTokens: 8, Provider: "secondary"}, nil
}

func (m *secondaryMockClient) SendChat(ctx context.Context, history []*llm.Content, tools []*tools.ToolDeclaration, resolver llm.AssetResolver) (*llm.Content, *llm.Metrics, error) {
	return nil, nil, nil
}

func (m *secondaryMockClient) GenerateImages(ctx context.Context, model, prompt, mimeType string) ([][]byte, error) {
	return nil, nil
}

func (m *secondaryMockClient) RefreshAuth() error { return nil }

// TestFailover_PrimaryTransient_SecondarySucceeds verifies that when the
// primary client returns a transient error, the FailoverGateway falls
// through to the secondary client and returns its successful response.
func TestFailover_PrimaryTransient_SecondarySucceeds(t *testing.T) {
	// Step 1 — Setup: create mock clients with atomic call counters
	var primaryCalls, secondaryCalls int32

	primary := &primaryMockClient{callCount: &primaryCalls}
	secondary := &secondaryMockClient{callCount: &secondaryCalls}

	// Step 2 — Create FailoverGateway with ordered client list
	fg := infra_llm.NewFailoverGateway([]infra_llm.NamedClient{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	})

	// Step 3 — Execute Generate (triggers failover)
	ctx := context.Background()
	content, metrics, err := fg.Generate(ctx, nil, nil, nil)

	// Step 4 — Assert no error
	if err != nil {
		t.Fatalf("Generate failed: %v", err)
	}

	// Step 5 — Assert model response contains "Failover successful"
	if content == nil || len(content.Parts) == 0 {
		t.Fatal("expected non-empty content")
	}
	if content.Parts[0].Text != "Failover successful — response from secondary!" {
		t.Errorf("got response %q, want %q", content.Parts[0].Text, "Failover successful — response from secondary!")
	}

	// Step 6 — Assert metrics reflect secondary provider
	if metrics != nil && metrics.Provider != "secondary" {
		t.Errorf("got provider %q, want %q", metrics.Provider, "secondary")
	}

	// Step 7 — Assert call counts: primary tried once, secondary succeeded
	if got := atomic.LoadInt32(&primaryCalls); got != 1 {
		t.Errorf("primaryCalls = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&secondaryCalls); got != 1 {
		t.Errorf("secondaryCalls = %d, want 1", got)
	}
}

// TestFailover_Anthropic503_OpenAISucceeds is a full CLI E2E test that
// exercises provider failover through the compiled binary. Two httptest
// servers simulate a primary that always returns 503 and a secondary that
// returns a successful text response. The test verifies that the CLI tries
// the primary, falls through on the transient error, and returns the
// secondary's response.
//
// Both providers use TYPE "google" so per-request format is identical.
// VertexAI-style URLs (containing aiplatform.googleapis.com as a path
// segment) are used to trigger parseVertexAI in determineBackend, which
// extracts a per-provider baseURL from the config URL itself — avoiding
// the single-process-wide TELL_ME_MOCK_URL constraint.
func TestFailover_Anthropic503_OpenAISucceeds(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping slow E2E test in short mode")
	}

	// 1. Primary mock server — always returns 503
	var primaryCalls int32
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = fmt.Fprint(w, `{"error": {"code": 503, "message": "Service Unavailable"}}`)
	}))
	defer primaryServer.Close()

	// 2. Secondary mock server — returns success
	var secondaryCalls int32
	secondaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&secondaryCalls, 1)
		w.Header().Set("Content-Type", "application/json")
		resp := createTextResponse("google", "Failover successful — secondary responded!")
		_, _ = fmt.Fprint(w, resp)
	}))
	defer secondaryServer.Close()

	// 3. Create temp config with two providers and FAILOVER_ORDER.
	// Use VertexAI-style URLs so determineBackend triggers parseVertexAI
	// which extracts per-provider baseURL from the URL itself.
	primaryURL := primaryServer.URL + "/aiplatform.googleapis.com/v1/projects/mock/locations/mock"
	secondaryURL := secondaryServer.URL + "/aiplatform.googleapis.com/v1/projects/mock/locations/mock"

	configContent := fmt.Sprintf(`
MODE: "assistant"
SELECTED_PROVIDER: "mock_primary"
PROVIDERS:
  mock_primary:
    TYPE: "google"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
  mock_secondary:
    TYPE: "google"
    URL: "%s"
    API_KEY: "dummy"
    MODEL: "gpt-4"
FAILOVER_ORDER:
  - mock_primary
  - mock_secondary
`, primaryURL, secondaryURL)

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatal(err)
	}

	// 4. Run the CLI — TELL_ME_FAST_RETRY=1 is injected by runCommandWithEnvInDir
	homeDir := t.TempDir()
	env := []string{"TELL_ME_HOME=" + homeDir}

	stdout, stderr, err := runCommandWithEnvInDir(homeDir, env, "", "-c", configPath, "failover test")
	if err != nil {
		t.Fatalf("CLI failed: %v\nStderr: %s", err, stderr)
	}

	// 5. Assertions
	if !strings.Contains(stdout, "Failover successful — secondary responded!") {
		t.Errorf("expected stdout to contain success message, got: %q", stdout)
	}
	if got := atomic.LoadInt32(&primaryCalls); got < 1 {
		t.Errorf("primaryCalls = %d, want >= 1", got)
	}
	if got := atomic.LoadInt32(&secondaryCalls); got < 1 {
		t.Errorf("secondaryCalls = %d, want >= 1", got)
	}
}
