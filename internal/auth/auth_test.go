// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import (
	"os"
	"strings"
	"testing"
)

func TestVertexAuth(t *testing.T) {
	t.Parallel()
	auth := &VertexAuth{Token: "test-token"}
	req := &Request{
		Headers: make(map[string]string),
	}
	auth.Apply(req)

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
	if !strings.Contains(path, "tell_me_go_token_vertex_") {
		t.Errorf("expected path to contain prefix, got %s", path)
	}
}

func TestVertexAuth_Invalidate(t *testing.T) {
	auth := &VertexAuth{Token: "some-token"}
	cachePath := auth.getCachePath()
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
		auth := &VertexAuth{}
		cachePath := auth.getCachePath()
		_ = os.WriteFile(cachePath, []byte("cached-token"), 0600)
		defer os.Remove(cachePath)

		token, err := auth.GetToken()
		if err != nil {
			t.Fatalf("GetToken failed: %v", err)
		}
		if token != "cached-token" {
			t.Errorf("got %s, want cached-token", token)
		}
	})

	t.Run("Fetch from gcloud", func(t *testing.T) {
		auth := &VertexAuth{
			tokenCmdFunc: func() ([]byte, error) {
				return []byte("gcloud-token"), nil
			},
		}
		cachePath := auth.getCachePath()
		_ = os.Remove(cachePath) // ensure no cache
		defer os.Remove(cachePath)

		token, err := auth.GetToken()
		if err != nil {
			t.Fatalf("GetToken failed: %v", err)
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
