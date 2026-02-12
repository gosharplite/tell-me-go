// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"
)

// MemoryKVStore is an in-memory implementation of services.KVStore.
type MemoryKVStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func NewMemoryKVStore() *MemoryKVStore {
	return &MemoryKVStore{
		data: make(map[string]string),
	}
}

func (s *MemoryKVStore) Get(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key], nil
}

func (s *MemoryKVStore) Set(ctx context.Context, key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *MemoryKVStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *MemoryKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make(map[string]string, len(s.data))
	for k, v := range s.data {
		res[k] = v
	}
	return res, nil
}

// MemoryListStore is an in-memory implementation of services.ListStore.
type MemoryListStore[T any] struct {
	mu   sync.RWMutex
	data []T
}

func NewMemoryListStore[T any]() *MemoryListStore[T] {
	return &MemoryListStore[T]{}
}

func (s *MemoryListStore[T]) ReadAll(ctx context.Context) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]T, len(s.data))
	copy(res, s.data)
	return res, nil
}

func (s *MemoryListStore[T]) WriteAll(ctx context.Context, items []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make([]T, len(items))
	copy(s.data, items)
	return nil
}

func (s *MemoryListStore[T]) Append(ctx context.Context, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, item)
	return nil
}
