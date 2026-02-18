// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestVertexAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	auth := &VertexAuth{Token: "test-token"}
	req := &Request{
		Headers: make(map[string]string),
	}
	if err := auth.Apply(ctx, req); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if req.Headers["Authorization"] != "Bearer test-token" {
		t.Errorf("expected bearer token, got '%s'", req.Headers["Authorization"])
	}
}

func TestGetCachePath(t *testing.T) {
	auth := &VertexAuth{}
	path := auth.getCachePath()
	if path == "" {
		t.Fatal("expected non-empty cache path")
	}
	if !strings.Contains(path, "tell-me-go-auth-") {
		t.Errorf("expected path to contain prefix, got %s", path)
	}
	if !strings.HasSuffix(path, "token.txt") {
		t.Errorf("expected path to end with token.txt, got %s", path)
	}
}

func TestVertexAuth_Invalidate(t *testing.T) {
	auth := &VertexAuth{Token: "some-token"}
	cachePath := auth.getCachePath()
	_ = os.MkdirAll(filepath.Dir(cachePath), 0700)
	_ = os.WriteFile(cachePath, []byte("some-token"), 0600)

	auth.Invalidate()

	if auth.Token != "" {
		t.Error("expected Token to be cleared")
	}

	if _, err := os.Stat(cachePath); !os.IsNotExist(err) {
		t.Error("expected cache file to be deleted")
	}
}

func TestVertexAuth_GetToken(t *testing.T) {
	t.Run("Use cached token", func(t *testing.T) {
		ctx := context.Background()
		auth := &VertexAuth{}
		cachePath := auth.getCachePath()
		_ = os.MkdirAll(filepath.Dir(cachePath), 0700)
		_ = os.WriteFile(cachePath, []byte("cached-token"), 0600)
		defer os.Remove(cachePath)

		token, err := auth.getToken(ctx)
		if err != nil {
			t.Fatalf("getToken failed: %v", err)
		}
		if token != "cached-token" {
			t.Errorf("got %s, want cached-token", token)
		}
	})

	t.Run("Fetch from gcloud", func(t *testing.T) {
		ctx := context.Background()
		auth := &VertexAuth{
			tokenCmdFunc: func() ([]byte, error) {
				return []byte("gcloud-token"), nil
			},
		}
		cachePath := auth.getCachePath()
		_ = os.Remove(cachePath) // ensure no cache
		defer os.Remove(cachePath)

		token, err := auth.getToken(ctx)
		if err != nil {
			t.Fatalf("getToken failed: %v", err)
		}
		if token != "gcloud-token" {
			t.Errorf("got %s, want gcloud-token", token)
		}

		// Verify it was cached
		content, _ := os.ReadFile(cachePath)
		if strings.TrimSpace(string(content)) != "gcloud-token" {
			t.Errorf("token not cached correctly: %s", string(content))
		}
	})
}

func TestServiceAccountAuth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	auth := &ServiceAccountAuth{
		token:  "cached-sa-token",
		expiry: time.Now().Add(10 * time.Minute),
	}

	t.Run("Use cached token", func(t *testing.T) {
		token, err := auth.getToken(ctx)
		if err != nil {
			t.Fatalf("getToken failed: %v", err)
		}
		if token != "cached-sa-token" {
			t.Errorf("got %s, want cached-sa-token", token)
		}
	})

	t.Run("Invalidate", func(t *testing.T) {
		auth.Invalidate()
		if auth.token != "" {
			t.Error("expected token to be cleared")
		}
	})

	t.Run("Apply cached token", func(t *testing.T) {
		auth.token = "sa-token"
		auth.expiry = time.Now().Add(10 * time.Minute)
		req := &Request{Headers: make(map[string]string)}
		if err := auth.Apply(ctx, req); err != nil {
			t.Fatalf("Apply failed: %v", err)
		}
		if req.Headers["Authorization"] != "Bearer sa-token" {
			t.Errorf("got %s, want Bearer sa-token", req.Headers["Authorization"])
		}
	})
}

func TestVertexAuth_Concurrency(t *testing.T) {
	ctx := context.Background()
	auth := &VertexAuth{
		tokenCmdFunc: func() ([]byte, error) {
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
			return []byte("concurrent-token"), nil
		},
	}
	cachePath := auth.getCachePath()
	_ = os.Remove(cachePath)
	defer os.Remove(cachePath)

	const n = 10
	errChan := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() {
			req := &Request{Headers: make(map[string]string)}
			err := auth.Apply(ctx, req)
			if err == nil {
				if req.Headers["Authorization"] != "Bearer concurrent-token" {
					err = fmt.Errorf("unexpected header: %s", req.Headers["Authorization"])
				}
			}
			errChan <- err
		}()
	}

	for i := 0; i < n; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("concurrency error: %v", err)
		}
	}
}

func TestServiceAccountAuth_TokenExchange(t *testing.T) {
	t.Parallel()

	t.Run("Successful Exchange", testSA_SuccessfulExchange)
	t.Run("Expiration Handling", testSA_ExpirationHandling)
	t.Run("Error Handling", testSA_ErrorHandling)
	t.Run("Thread Safety", testSA_ThreadSafety)
	t.Run("Production Branch - File Not Found", testSA_ProductionBranch_FileNotFound)
	t.Run("Production Branch - Invalid JSON", testSA_ProductionBranch_InvalidJSON)
	t.Run("Production Branch - Invalid Private Key", testSA_ProductionBranch_InvalidPrivateKey)
}

func testSA_SuccessfulExchange(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			callCount++
			return &oauth2.Token{
				AccessToken: "mock-token",
				Expiry:      time.Now().Add(1 * time.Hour),
			}, nil
		},
	}

	req := &Request{Headers: make(map[string]string)}
	if err := auth.Apply(ctx, req); err != nil {
		t.Fatalf("Apply failed: %v", err)
	}

	if req.Headers["Authorization"] != "Bearer mock-token" {
		t.Errorf("got %s, want Bearer mock-token", req.Headers["Authorization"])
	}

	// Second call should use cache
	if err := auth.Apply(ctx, req); err != nil {
		t.Fatalf("Apply 2 failed: %v", err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call to tokenSourceFunc, got %d", callCount)
	}
}

func testSA_ExpirationHandling(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			callCount++
			// First token expires soon, second one is fresh
			expiry := time.Now().Add(4 * time.Minute)
			if callCount > 1 {
				expiry = time.Now().Add(1 * time.Hour)
			}
			return &oauth2.Token{
				AccessToken: fmt.Sprintf("token-%d", callCount),
				Expiry:      expiry,
			}, nil
		},
	}

	// First call
	token1, _ := auth.getToken(ctx)
	if token1 != "token-1" {
		t.Errorf("got %s, want token-1", token1)
	}

	// Second call should trigger refresh because token-1 is within 5-min buffer
	token2, _ := auth.getToken(ctx)
	if token2 != "token-2" {
		t.Errorf("got %s, want token-2", token2)
	}
	if callCount != 2 {
		t.Errorf("expected 2 calls, got %d", callCount)
	}
}

func testSA_ErrorHandling(t *testing.T) {
	ctx := context.Background()
	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			return nil, fmt.Errorf("provider error")
		},
	}
	_, err := auth.getToken(ctx)
	if err == nil || !strings.Contains(err.Error(), "provider error") {
		t.Errorf("expected provider error, got %v", err)
	}
}

func testSA_ThreadSafety(t *testing.T) {
	ctx := context.Background()
	callCount := 0
	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			time.Sleep(10 * time.Millisecond) // Ensure overlap
			callCount++
			return &oauth2.Token{
				AccessToken: "concurrent-token",
				Expiry:      time.Now().Add(1 * time.Hour),
			}, nil
		},
	}

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = auth.getToken(ctx)
		}()
	}
	wg.Wait()

	if callCount != 1 {
		t.Errorf("expected only 1 call to tokenSourceFunc, got %d", callCount)
	}
}

func testSA_ProductionBranch_FileNotFound(t *testing.T) {
	ctx := context.Background()
	auth := &ServiceAccountAuth{
		KeyFilePath: "non-existent.json",
	}
	_, err := auth.getToken(ctx)
	if err == nil || !strings.Contains(err.Error(), "failed to read service account key") {
		t.Errorf("expected file not found error, got %v", err)
	}
}

func testSA_ProductionBranch_InvalidJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "invalid-key.json")
	_ = os.WriteFile(keyFile, []byte("invalid json"), 0600)

	auth := &ServiceAccountAuth{
		KeyFilePath: keyFile,
	}
	_, err := auth.getToken(ctx)
	if err == nil || !strings.Contains(err.Error(), "failed to parse service account JSON") {
		t.Errorf("expected parse error, got %v", err)
	}
}

func testSA_ProductionBranch_InvalidPrivateKey(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	keyFile := filepath.Join(tmpDir, "invalid-key.json")
	content := `{
  "type": "service_account",
  "project_id": "test-project",
  "private_key": "not-a-pem-key",
  "client_email": "test@test-project.iam.gserviceaccount.com",
  "token_uri": "https://oauth2.googleapis.com/token"
}`
	_ = os.WriteFile(keyFile, []byte(content), 0600)

	auth := &ServiceAccountAuth{
		KeyFilePath: keyFile,
	}
	_, err := auth.getToken(ctx)
	// google-cloud-go's CredentialsFromJSON might fail to parse the key
	if err == nil {
		t.Errorf("expected error, got nil")
	}
}


func TestOtherAuthenticators(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("APIKeyAuth", func(t *testing.T) {
		auth := &APIKeyAuth{APIKey: "test-api-key"}
		req := &Request{Headers: make(map[string]string)}
		_ = auth.Apply(ctx, req)
		if req.Headers["x-goog-api-key"] != "test-api-key" {
			t.Errorf("got %s, want test-api-key", req.Headers["x-goog-api-key"])
		}
		token, _ := auth.getToken(ctx)
		if token != "test-api-key" {
			t.Errorf("got %s, want test-api-key", token)
		}
		auth.Invalidate() // should do nothing
	})

	t.Run("BearerAuth", func(t *testing.T) {
		auth := &BearerAuth{Token: "test-bearer"}
		req := &Request{Headers: make(map[string]string)}
		_ = auth.Apply(ctx, req)
		if req.Headers["Authorization"] != "Bearer test-bearer" {
			t.Errorf("got %s, want Bearer test-bearer", req.Headers["Authorization"])
		}
		token, _ := auth.getToken(ctx)
		if token != "test-bearer" {
			t.Errorf("got %s, want test-bearer", token)
		}
		auth.Invalidate() // should do nothing
	})

	t.Run("AnthropicAuth", func(t *testing.T) {
		auth := &AnthropicAuth{APIKey: "test-anthropic"}
		req := &Request{Headers: make(map[string]string)}
		_ = auth.Apply(ctx, req)
		if req.Headers["x-api-key"] != "test-anthropic" {
			t.Errorf("got %s, want test-anthropic", req.Headers["x-api-key"])
		}
		token, _ := auth.getToken(ctx)
		if token != "test-anthropic" {
			t.Errorf("got %s, want test-anthropic", token)
		}
		auth.Invalidate() // should do nothing
	})
}
