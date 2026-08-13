// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package ports

import "context"

// Authenticator defines the interface for injecting credentials into API requests.
type Authenticator interface {
	Invalidate()
	Apply(ctx context.Context, req *Request) error
}

// Request is a wrapper for the headers needed to apply authentication.
type Request struct {
	Headers map[string]string
}
