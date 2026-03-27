// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package security

import "context"

type authContextKey struct{}

// WithApproval returns a new context with the authorization approval state injected.
func WithApproval(ctx context.Context, approved bool) context.Context {
	return context.WithValue(ctx, authContextKey{}, approved)
}

// IsApproved returns true if the context contains a granted authorization approval.
func IsApproved(ctx context.Context) bool {
	if approved, ok := ctx.Value(authContextKey{}).(bool); ok {
		return approved
	}
	return false
}
