// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import "context"

// AssetStore manages binary blobs in content-addressable storage.
//
// It is the domain port that the infrastructure/history adapter depends on
// to offload binary InlineData parts. The concrete implementation lives in
// internal/infrastructure/persistence; this interface keeps history decoupled
// from that concrete type (issue #1350, item 5).
type AssetStore interface {
	Put(ctx context.Context, data []byte) (string, error)
	Get(ctx context.Context, id string) ([]byte, error)
}
