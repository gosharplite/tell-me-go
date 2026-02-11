// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package services

import "context"

// KVStore defines a generic key-value storage interface.
type KVStore interface {
	Get(ctx context.Context, key string) (string, error)
	Set(ctx context.Context, key string, val string) error
	Delete(ctx context.Context, key string) error
	GetAll(ctx context.Context) (map[string]string, error)
}

// ListStore defines a generic list storage interface.
type ListStore[T any] interface {
	ReadAll(ctx context.Context) ([]T, error)
	WriteAll(ctx context.Context, items []T) error
}
