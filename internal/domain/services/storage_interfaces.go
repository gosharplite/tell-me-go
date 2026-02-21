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
	ReadPage(ctx context.Context, limit, offset int) ([]T, error)
	Append(ctx context.Context, item T) error
	Update(ctx context.Context, id float64, item T) error
	Delete(ctx context.Context, id float64) error
	DeleteAll(ctx context.Context) error
}
