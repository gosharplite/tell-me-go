// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestVertexAuth(t *testing.T) {
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
	auth := &VertexAuth{Token: "some-token", CacheDir: t.TempDir()}
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

func TestVertexAuth_Invalidate_RemoveError(t *testing.T) {
	t.Run("remove failure logs but clears token", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Chmod 0555 on directory not reliable on Windows")
		}

		tmpDir := t.TempDir()
		cacheDir := filepath.Join(tmpDir, "cache")
		auth := &VertexAuth{Token: "some-token", CacheDir: cacheDir}
		cachePath := auth.getCachePath()

		// Create cache directory and file
		if err := os.MkdirAll(cacheDir, 0700); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}
		if err := os.WriteFile(cachePath, []byte("token"), 0600); err != nil {
			t.Fatalf("failed to write cache file: %v", err)
		}

		// Make parent directory read-only so os.Remove fails
		if err := os.Chmod(cacheDir, 0555); err != nil {
			t.Fatalf("failed to chmod cache dir: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(cacheDir, 0700)
		})

		auth.Invalidate()

		// Primary contract: in-memory token cleared even when file removal fails
		if auth.Token != "" {
			t.Error("expected Token to be cleared even when file removal fails")
		}

		// The cache file should still exist (removal silently failed)
		if _, err := os.Stat(cachePath); os.IsNotExist(err) {
			t.Error("expected cache file to still exist when removal fails")
		}
	})
}

func TestVertexAuth_GetToken(t *testing.T) {
	t.Run("Use cached token", func(t *testing.T) {
		ctx := context.Background()
		auth := &VertexAuth{CacheDir: t.TempDir()}
		cachePath := auth.getCachePath()
		_ = os.MkdirAll(filepath.Dir(cachePath), 0700)
		_ = os.WriteFile(cachePath, []byte("cached-token"), 0600)

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
			CacheDir: t.TempDir(),
			tokenCmdFunc: func() ([]byte, error) {
				return []byte("gcloud-token"), nil
			},
		}
		cachePath := auth.getCachePath()

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
	var calls int32
	inFunc := make(chan struct{})
	release := make(chan struct{})

	auth := &VertexAuth{
		CacheDir: t.TempDir(),
		tokenCmdFunc: func() ([]byte, error) {
			atomic.AddInt32(&calls, 1)
			inFunc <- struct{}{}
			<-release
			return []byte("concurrent-token"), nil
		},
	}
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

	<-inFunc
	close(release)

	for i := 0; i < n; i++ {
		if err := <-errChan; err != nil {
			t.Errorf("concurrency error: %v", err)
		}
	}

	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected only 1 call, got %d", calls)
	}
}

func TestServiceAccountAuth_TokenExchange(t *testing.T) {

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
	var calls int32
	inFunc := make(chan struct{})
	release := make(chan struct{})

	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			atomic.AddInt32(&calls, 1)
			inFunc <- struct{}{}
			<-release
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

	<-inFunc
	close(release)
	wg.Wait()

	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected only 1 call to tokenSourceFunc, got %d", calls)
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
	ctx := context.Background()

	t.Run("APIKeyAuth", func(t *testing.T) {
		auth := &APIKeyAuth{APIKey: "test-api-key"}
		req := &Request{Headers: make(map[string]string)}
		_ = auth.Apply(ctx, req)
		if req.Headers["x-goog-api-key"] != "test-api-key" {
			t.Errorf("got %s, want test-api-key", req.Headers["x-goog-api-key"])
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
		auth.Invalidate() // should do nothing
	})

	t.Run("AnthropicAuth", func(t *testing.T) {
		auth := &AnthropicAuth{APIKey: "test-anthropic"}
		req := &Request{Headers: make(map[string]string)}
		_ = auth.Apply(ctx, req)
		if req.Headers["x-api-key"] != "test-anthropic" {
			t.Errorf("got %s, want test-anthropic", req.Headers["x-api-key"])
		}
		auth.Invalidate() // should do nothing
	})
}

func TestNoOpAuth(t *testing.T) {
	a := &noOpAuth{}

	// Should not panic
	a.Invalidate()

	// Should return nil error
	req := &Request{Headers: make(map[string]string)}
	err := a.Apply(context.Background(), req)
	if err != nil {
		t.Errorf("Apply() expected nil error, got %v", err)
	}
}

func TestVertexAuth_GetCachePath_WindowsFallback(t *testing.T) {
	originalGetUID := getUID
	getUID = func() int { return -1 }
	t.Cleanup(func() { getUID = originalGetUID })

	t.Setenv("USERNAME", "testwindowsuser")
	t.Setenv("USER", "should-not-be-used")

	auth := &VertexAuth{}
	path := auth.getCachePath()
	if !strings.Contains(path, "testwindowsuser") {
		t.Errorf("expected path to contain USERNAME 'testwindowsuser', got %s", path)
	}
}

func TestVertexAuth_GetCachePath_UnixFallback(t *testing.T) {
	originalGetUID := getUID
	getUID = func() int { return -1 }
	t.Cleanup(func() { getUID = originalGetUID })

	t.Setenv("USERNAME", "")
	t.Setenv("USER", "testunixuser")

	auth := &VertexAuth{}
	path := auth.getCachePath()
	if !strings.Contains(path, "testunixuser") {
		t.Errorf("expected path to contain USER 'testunixuser', got %s", path)
	}
}

func TestVertexAuth_GetCachePath_AllFallbacksEmpty(t *testing.T) {
	originalGetUID := getUID
	getUID = func() int { return -1 }
	t.Cleanup(func() { getUID = originalGetUID })

	t.Setenv("USERNAME", "")
	t.Setenv("USER", "")

	auth := &VertexAuth{}
	path := auth.getCachePath()
	if !strings.Contains(path, "tell-me-go-auth--1") {
		t.Errorf("expected path to contain 'tell-me-go-auth--1', got %s", path)
	}
	if !strings.HasSuffix(path, "token.txt") {
		t.Errorf("expected path to end with 'token.txt', got %s", path)
	}
}

func TestVertexAuth_GetToken_GcloudError(t *testing.T) {
	ctx := context.Background()
	auth := &VertexAuth{
		CacheDir: t.TempDir(),
		tokenCmdFunc: func() ([]byte, error) {
			return nil, fmt.Errorf("gcloud failure")
		},
	}

	_, err := auth.getToken(ctx)
	if err == nil || !strings.Contains(err.Error(), "failed to get gcloud token") {
		t.Errorf("expected gcloud token error, got %v", err)
	}
}

func TestVertexAuth_Apply_Error(t *testing.T) {
	ctx := context.Background()
	auth := &VertexAuth{
		CacheDir: t.TempDir(),
		tokenCmdFunc: func() ([]byte, error) {
			return nil, fmt.Errorf("mock gcloud error")
		},
	}

	req := &Request{Headers: make(map[string]string)}
	err := auth.Apply(ctx, req)

	if err == nil || !strings.Contains(err.Error(), "mock gcloud error") {
		t.Errorf("Expected mock gcloud error, got %v", err)
	}
}

func TestServiceAccountAuth_Apply_Error(t *testing.T) {
	ctx := context.Background()
	auth := &ServiceAccountAuth{
		tokenSourceFunc: func() (*oauth2.Token, error) {
			return nil, fmt.Errorf("mock oauth2 error")
		},
	}

	req := &Request{Headers: make(map[string]string)}
	err := auth.Apply(ctx, req)

	if err == nil || !strings.Contains(err.Error(), "mock oauth2 error") {
		t.Errorf("Expected mock oauth2 error, got %v", err)
	}
}

func TestAuthInvalidate_Additional(t *testing.T) {
	t.Run("APIKeyAuth_Invalidate", func(t *testing.T) {
		auth := &APIKeyAuth{APIKey: "test-key"}
		auth.Invalidate()
		if auth.APIKey != "test-key" {
			t.Errorf("APIKeyAuth.Invalidate should not clear the key, but it did")
		}
	})

	t.Run("BearerAuth_Invalidate", func(t *testing.T) {
		auth := &BearerAuth{Token: "test-token"}
		auth.Invalidate()
		if auth.Token != "test-token" {
			t.Errorf("BearerAuth.Invalidate should not clear the token, but it did")
		}
	})

	t.Run("AnthropicAuth_Invalidate", func(t *testing.T) {
		auth := &AnthropicAuth{APIKey: "test-key"}
		auth.Invalidate()
		if auth.APIKey != "test-key" {
			t.Errorf("AnthropicAuth.Invalidate should not clear the key, but it did")
		}
	})
}

func TestEmptyCredentials_NoHeaderAdded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("APIKeyAuth empty key", func(t *testing.T) {
		t.Parallel()
		auth := &APIKeyAuth{APIKey: ""}
		req := &Request{Headers: make(map[string]string)}
		err := auth.Apply(ctx, req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(req.Headers) != 0 {
			t.Errorf("expected no headers with empty API key, got: %v", req.Headers)
		}
	})

	t.Run("BearerAuth empty token", func(t *testing.T) {
		t.Parallel()
		auth := &BearerAuth{Token: ""}
		req := &Request{Headers: make(map[string]string)}
		err := auth.Apply(ctx, req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(req.Headers) != 0 {
			t.Errorf("expected no headers with empty token, got: %v", req.Headers)
		}
	})

	t.Run("AnthropicAuth empty key", func(t *testing.T) {
		t.Parallel()
		auth := &AnthropicAuth{APIKey: ""}
		req := &Request{Headers: make(map[string]string)}
		err := auth.Apply(ctx, req)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if len(req.Headers) != 0 {
			t.Errorf("expected no headers with empty API key, got: %v", req.Headers)
		}
	})
}

func TestVertexAuth_GetToken_ExpiredCache(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	auth := &VertexAuth{
		CacheDir: tmpDir,
		tokenCmdFunc: func() ([]byte, error) {
			return []byte("new-token"), nil
		},
	}
	cachePath := auth.getCachePath()
	_ = os.MkdirAll(filepath.Dir(cachePath), 0700)
	_ = os.WriteFile(cachePath, []byte("expired-token"), 0600)

	// Set mod time to 2 hours ago
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cachePath, past, past); err != nil {
		t.Fatalf("failed to set cache time: %v", err)
	}

	token, err := auth.getToken(ctx)
	if err != nil {
		t.Fatalf("getToken failed: %v", err)
	}
	if token != "new-token" {
		t.Errorf("got %s, want new-token", token)
	}

	// Verify it was updated in cache
	content, _ := os.ReadFile(cachePath)
	if strings.TrimSpace(string(content)) != "new-token" {
		t.Errorf("token not updated in cache: %s", string(content))
	}
}

func TestVertexAuth_CacheWriteFailure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	// Create a file where the directory should be, making MkdirAll fail
	conflictPath := filepath.Join(tmpDir, "tell-me-go-auth-conflict")
	_ = os.WriteFile(conflictPath, []byte("not a dir"), 0600)

	auth := &VertexAuth{
		CacheDir: conflictPath,
		tokenCmdFunc: func() ([]byte, error) {
			return []byte("resilient-token"), nil
		},
	}

	token, err := auth.getToken(ctx)
	if err != nil {
		t.Fatalf("getToken failed: %v", err)
	}
	if token != "resilient-token" {
		t.Errorf("got %s, want resilient-token", token)
	}
}

func TestVertexAuth_GetToken_AtomicWriteFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chmod 0555 not reliable on Windows")
	}

	ctx := context.Background()
	tmpDir := t.TempDir()

	// Compute the cache path to find the directory that needs to exist but be read-only
	auth := &VertexAuth{CacheDir: tmpDir}
	cachePath := auth.getCachePath()
	cacheDir := filepath.Dir(cachePath)

	// Create the cache directory
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		t.Fatalf("failed to create cache dir: %v", err)
	}

	// Make it read-only so that AtomicWrite (CreateTemp) fails inside
	if err := os.Chmod(cacheDir, 0555); err != nil {
		t.Fatalf("failed to chmod cache dir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(cacheDir, 0700)
	})

	// Fresh VertexAuth — no preset Token, so code path goes:
	// cache miss → gcloud → MkdirAll(succeeds) → AtomicWrite(fails)
	auth2 := &VertexAuth{
		CacheDir: tmpDir,
		tokenCmdFunc: func() ([]byte, error) {
			return []byte("resilient-token"), nil
		},
	}

	token, err := auth2.getToken(ctx)
	if err != nil {
		t.Fatalf("getToken should return token despite AtomicWrite failure, got error: %v", err)
	}
	if token != "resilient-token" {
		t.Errorf("got %q, want resilient-token", token)
	}
}

func TestNewVertexAuth_DefaultTokenCmdFunc(t *testing.T) {
	a := NewVertexAuth()
	if a.tokenCmdFunc == nil {
		t.Fatal("NewVertexAuth must wire a default tokenCmdFunc")
	}
}

// TestVertexAuth_NilTokenCmdFunc_ReturnsError verifies that calling getToken()
// on a bare &VertexAuth{} without a wired tokenCmdFunc returns a descriptive error
// instead of panicking. This guards against future test code that constructs
// VertexAuth without using NewVertexAuth() or presetting Token.
func TestVertexAuth_NilTokenCmdFunc_ReturnsError(t *testing.T) {
	auth := &VertexAuth{
		CacheDir: t.TempDir(), // isolate from any stale cached token on disk
	}
	// No Token set, no tokenCmdFunc wired — should hit the nil guard

	_, err := auth.getToken(context.Background())

	if err == nil {
		t.Fatal("expected error for nil tokenCmdFunc, got nil")
	}
	if !strings.Contains(err.Error(), "NewVertexAuth") {
		t.Errorf("error should mention NewVertexAuth, got: %v", err)
	}
}

func TestServiceAccountAuth_EmptyKeyFilePath(t *testing.T) {
	tests := []struct {
		name string
		auth *ServiceAccountAuth
		want string // expected substring in error, empty means no error expected
	}{
		{
			name: "empty KeyFilePath returns descriptive error",
			auth: &ServiceAccountAuth{},
			want: "KeyFilePath is empty",
		},
		{
			name: "empty KeyFilePath with tokenSourceFunc still works",
			auth: &ServiceAccountAuth{
				tokenSourceFunc: func() (*oauth2.Token, error) {
					return &oauth2.Token{
						AccessToken: "mock-token",
						Expiry:      time.Now().Add(1 * time.Hour),
					}, nil
				},
			},
			want: "", // no error expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := tt.auth.getToken(context.Background())
			if tt.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if token != "mock-token" {
					t.Errorf("got %q, want %q", token, "mock-token")
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.want) {
					t.Errorf("expected error containing %q, got %q", tt.want, err.Error())
				}
			}
		})
	}
}

func TestVertexAuth_GetToken_CorruptCache(t *testing.T) {
	t.Run("corrupt cache falls back to gcloud", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Chmod 0000 not reliable on Windows")
		}

		ctx := context.Background()
		tmpDir := t.TempDir()

		auth := &VertexAuth{
			CacheDir: tmpDir,
			tokenCmdFunc: func() ([]byte, error) {
				return []byte("fresh-gcloud-token"), nil
			},
		}

		cachePath := auth.getCachePath()
		dir := filepath.Dir(cachePath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatalf("failed to create cache dir: %v", err)
		}
		if err := os.WriteFile(cachePath, []byte("stale-token"), 0600); err != nil {
			t.Fatalf("failed to write cache file: %v", err)
		}

		// Make the cache file unreadable to simulate corruption
		if err := os.Chmod(cachePath, 0000); err != nil {
			t.Fatalf("failed to chmod cache file: %v", err)
		}

		// Restore permissions so t.TempDir cleanup can remove the file
		t.Cleanup(func() {
			_ = os.Chmod(cachePath, 0600)
		})

		token, err := auth.getToken(ctx)
		if err != nil {
			t.Fatalf("getToken should fall back to gcloud, got error: %v", err)
		}
		if token != "fresh-gcloud-token" {
			t.Errorf("got %q, want fresh-gcloud-token", token)
		}

		// Verify the corrupt cache was overwritten with the fresh token
		// (restore readability first)
		if err := os.Chmod(cachePath, 0600); err != nil {
			t.Fatalf("failed to restore cache file permissions: %v", err)
		}
		content, err := os.ReadFile(cachePath)
		if err != nil {
			t.Fatalf("failed to read cache file after getToken: %v", err)
		}
		if strings.TrimSpace(string(content)) != "fresh-gcloud-token" {
			t.Errorf("cache file contains %q, want fresh-gcloud-token", string(content))
		}
	})
}

// TestNewVertexAuth_DefaultTokenCmdFunc_ExecError verifies that the default
// tokenCmdFunc wired by NewVertexAuth returns an error (not panics) when the
// underlying execCommand fails. This covers the ERROR_HANDLING gap at
// auth.go ~54-56 where execCommand("gcloud", "auth", "print-access-token").Output()
// can fail when gcloud is not installed or misconfigured.
func TestNewVertexAuth_DefaultTokenCmdFunc_ExecError(t *testing.T) {
	originalExecCommand := execCommand
	t.Cleanup(func() { execCommand = originalExecCommand })

	// Replace execCommand with a function that always returns an error exit.
	// Using "false" (which exits 1 on all platforms) as a portable stand-in
	// for a missing/failing gcloud.
	execCommand = func(name string, arg ...string) *exec.Cmd {
		return exec.Command("false")
	}

	a := NewVertexAuth()
	if a.tokenCmdFunc == nil {
		t.Fatal("NewVertexAuth must wire a non-nil tokenCmdFunc")
	}

	_, err := a.tokenCmdFunc()
	if err == nil {
		t.Error("expected error from default tokenCmdFunc when execCommand fails")
	}
}

// ---------------------------------------------------------------------------
// readCacheFile / writeCacheFile unit tests
// ---------------------------------------------------------------------------

func TestVertexAuth_ReadCacheFile_Hit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	auth := &VertexAuth{CacheDir: dir}
	cachePath := auth.getCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("  cached-token\n"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	token, ok := auth.readCacheFile()
	if !ok {
		t.Fatal("expected cache hit")
	}
	if token != "cached-token" {
		t.Errorf("got %q, want cached-token", token)
	}
}

func TestVertexAuth_ReadCacheFile_Missing(t *testing.T) {
	t.Parallel()

	auth := &VertexAuth{CacheDir: t.TempDir()}
	token, ok := auth.readCacheFile()
	if ok {
		t.Errorf("expected cache miss, got token=%q", token)
	}
	if token != "" {
		t.Errorf("expected empty token on miss, got %q", token)
	}
}

func TestVertexAuth_ReadCacheFile_Expired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	auth := &VertexAuth{CacheDir: dir}
	cachePath := auth.getCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("expired-token"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	// Set mod time to 2 hours ago → older than 55 minutes
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(cachePath, past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	token, ok := auth.readCacheFile()
	if ok {
		t.Errorf("expected cache miss for expired token, got %q", token)
	}
}

func TestVertexAuth_ReadCacheFile_Unreadable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Chmod 0000 not reliable on Windows")
	}
	t.Parallel()

	dir := t.TempDir()
	auth := &VertexAuth{CacheDir: dir}
	cachePath := auth.getCachePath()
	if err := os.MkdirAll(filepath.Dir(cachePath), 0700); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(cachePath, []byte("unreadable-token"), 0600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(cachePath, 0000); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(cachePath, 0600) })

	token, ok := auth.readCacheFile()
	if ok {
		t.Errorf("expected cache miss for unreadable file, got %q", token)
	}
}

func TestVertexAuth_WriteCacheFile(t *testing.T) {
	t.Run("successful write", func(t *testing.T) {
		dir := t.TempDir()
		auth := &VertexAuth{CacheDir: dir}
		auth.writeCacheFile(context.Background(), "my-token")

		cachePath := auth.getCachePath()
		content, err := os.ReadFile(cachePath)
		if err != nil {
			t.Fatalf("ReadFile after writeCacheFile: %v", err)
		}
		if string(content) != "my-token" {
			t.Errorf("got %q, want my-token", string(content))
		}
	})

	t.Run("mkdir failure does not panic", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Chmod 0555 not reliable on Windows")
		}
		dir := t.TempDir()
		// Create a read-only parent so MkdirAll fails
		if err := os.Chmod(dir, 0555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0700) })

		auth := &VertexAuth{CacheDir: filepath.Join(dir, "sub", "deep")}
		// Must not panic
		auth.writeCacheFile(context.Background(), "should-not-panic")
	})

	t.Run("write failure does not panic", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Chmod 0555 not reliable on Windows")
		}
		dir := t.TempDir()
		auth := &VertexAuth{CacheDir: dir}
		cachePath := auth.getCachePath()
		cacheDir := filepath.Dir(cachePath)
		if err := os.MkdirAll(cacheDir, 0700); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		// Make directory read-only so AtomicWrite cannot create temp file
		if err := os.Chmod(cacheDir, 0555); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(cacheDir, 0700) })

		// Must not panic
		auth.writeCacheFile(context.Background(), "should-not-panic")
	})
}
