// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

// Package contract defines the authentication seam shared by the LLM
// provider consumers and the auth implementers. It is a neutral contract
// leaf owned by the auth (concept-side) family per ADR-065.
package contract

import "context"

// AuthHeaders is the named map of HTTP headers produced by an
// Authenticator and consumed by provider clients.
type AuthHeaders map[string]string

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	Invalidate()
	Apply(ctx context.Context, headers AuthHeaders) error
}
