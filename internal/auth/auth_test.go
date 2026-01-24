// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import "testing"

func TestAPIKeyAuth(t *testing.T) {
	t.Parallel()
	auth := &APIKeyAuth{APIKey: "test-key"}
	req := &Request{
		QueryParams: make(map[string]string),
		Headers:     make(map[string]string),
	}
	auth.Apply(req)

	if req.QueryParams["key"] != "test-key" {
		t.Errorf("expected key 'test-key', got '%s'", req.QueryParams["key"])
	}
}

func TestVertexAuth(t *testing.T) {
	t.Parallel()
	auth := &VertexAuth{Token: "test-token"}
	req := &Request{
		QueryParams: make(map[string]string),
		Headers:     make(map[string]string),
	}
	auth.Apply(req)

	if req.Headers["Authorization"] != "Bearer test-token" {
		t.Errorf("expected bearer token, got '%s'", req.Headers["Authorization"])
	}
}
