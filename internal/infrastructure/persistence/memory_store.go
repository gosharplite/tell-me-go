// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"sync"
)

// MemoryKVStore is an in-memory implementation of services.KVStore.
type memoryKVStore struct {
	mu   sync.RWMutex
	data map[string]string
}

func newMemoryKVStore() *memoryKVStore {
	return &memoryKVStore{
		data: make(map[string]string),
	}
}

func (s *memoryKVStore) Get(ctx context.Context, key string) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data[key], nil
}

func (s *memoryKVStore) Set(ctx context.Context, key, val string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = val
	return nil
}

func (s *memoryKVStore) Delete(ctx context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
	return nil
}

func (s *memoryKVStore) GetAll(ctx context.Context) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make(map[string]string, len(s.data))
	for k, v := range s.data {
		res[k] = v
	}
	return res, nil
}

// memoryListStore is an in-memory implementation of services.ListStore.
type memoryListStore[T any] struct {
	mu   sync.RWMutex
	data []T
}

func newMemoryListStore[T any]() *memoryListStore[T] {
	return &memoryListStore[T]{}
}

func (s *memoryListStore[T]) ReadAll(ctx context.Context) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]T, len(s.data))
	copy(res, s.data)
	return res, nil
}

func (s *memoryListStore[T]) WriteAll(ctx context.Context, items []T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make([]T, len(items))
	copy(s.data, items)
	return nil
}

func (s *memoryListStore[T]) Append(ctx context.Context, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, item)
	return nil
}
