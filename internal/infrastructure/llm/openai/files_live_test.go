// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package openai

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
	"github.com/gosharplite/tell-me-go/internal/infrastructure/auth"
)

// TestKimiFilesContract_Live verifies end-to-end file upload, content
// extraction, and cleanup against the live Moonshot API. It is gated
// behind MOONSHOT_API_KEY + KIMI_LIVE_TEST=1 and never runs in CI.
//
// Run manually:
//
//	KIMI_LIVE_TEST=1 go test -run TestKimiFilesContract_Live -v -count=1 ./internal/infrastructure/llm/openai/
func TestKimiFilesContract_Live(t *testing.T) {
	apiKey := os.Getenv("MOONSHOT_API_KEY")
	if apiKey == "" || os.Getenv("KIMI_LIVE_TEST") != "1" {
		t.Skip("skipping live contract test: set MOONSHOT_API_KEY and KIMI_LIVE_TEST=1")
	}

	c := &client{
		baseURL:    "https://api.moonshot.ai/v1",
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     &ports.NoOpLogger{},
	}
	c.authenticator = &auth.BearerAuth{Token: apiKey}

	ctx := context.Background()

	// Upload a tiny text file and extract content end-to-end
	content, err := c.extractDocument(ctx, []byte("hello from live contract test"), "contract-test.txt")
	if err != nil {
		t.Fatalf("extractDocument against live API: %v", err)
	}

	if content == "" {
		t.Error("extracted content is empty — expected non-empty text")
	}

	t.Logf("extracted content: %s", content)
}
