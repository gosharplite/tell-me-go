// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package auth

import "testing"

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
