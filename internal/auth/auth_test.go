// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import (
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
