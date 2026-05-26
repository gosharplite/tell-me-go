// Copyright (c) 2026 gosharplite@gmail.com
// SPDX-License-Identifier: MIT

package persistence

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/gosharplite/tell-me-go/internal/domain/ports"
)

// MemoryKVStore is an in-memory implementation of ports.KVStore.
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

// memoryListStore is an in-memory implementation of ports.ListStore.
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

	// Collect active items (not completed) — all of them.
	var active []T
	for _, item := range s.data {
		if s.getStatus(item) == "completed" {
			continue
		}
		active = append(active, item)
	}

	// Collect completed items, keep only the most recent 500 (by position in slice).
	var completed []T
	for _, item := range s.data {
		if s.getStatus(item) == "completed" {
			completed = append(completed, item)
		}
	}
	if len(completed) > 500 {
		// Since data is append-ordered and IDs are auto-incrementing,
		// the last 500 completed items are the most recent.
		completed = completed[len(completed)-500:]
	}

	// Merge: active first, then completed. Both are already in append order (ID ASC).
	result := make([]T, 0, len(active)+len(completed))
	result = append(result, active...)
	result = append(result, completed...)

	return result, nil
}

func (s *memoryListStore[T]) getID(item T) int64 {
	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("ID")
		if field.IsValid() && field.CanInt() {
			return field.Int()
		}
	}
	return 0
}

func (s *memoryListStore[T]) getStatus(item T) string {
	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("Status")
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
	}
	return ""
}

func (s *memoryListStore[T]) getCreatedAt(item T) time.Time {
	val := reflect.ValueOf(item)
	if val.Kind() == reflect.Struct {
		field := val.FieldByName("CreatedAt")
		if field.IsValid() && field.Type() == reflect.TypeOf(time.Time{}) {
			return field.Interface().(time.Time)
		}
	}
	return time.Time{}
}

// matchesFilter returns true when item satisfies all non-zero filters.
func (s *memoryListStore[T]) matchesFilter(item T, filter ports.ListFilter) bool {
	if filter.Status != "" {
		if s.getStatus(item) != filter.Status {
			return false
		}
	}
	if filter.NotStatus != "" {
		if s.getStatus(item) == filter.NotStatus {
			return false
		}
	}
	if !filter.Since.IsZero() {
		if s.getCreatedAt(item).Before(filter.Since) {
			return false
		}
	}
	if !filter.Before.IsZero() {
		if s.getCreatedAt(item).After(filter.Before) {
			return false
		}
	}
	return true
}

// applyOffsetLimit slices result with the given offset and limit.
func applyOffsetLimit[T any](result []T, offset, limit int) []T {
	if offset > 0 {
		if offset >= len(result) {
			return []T{}
		}
		result = result[offset:]
	}
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}
	return result
}

func (s *memoryListStore[T]) Query(ctx context.Context, filter ports.ListFilter, limit, offset int) ([]T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []T
	for _, item := range s.data {
		if s.matchesFilter(item, filter) {
			result = append(result, item)
		}
	}
	return applyOffsetLimit(result, offset, limit), nil
}

func (s *memoryListStore[T]) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data), nil
}

func (s *memoryListStore[T]) Update(ctx context.Context, id int64, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.data {
		if s.getID(v) == id {
			s.data[i] = item
			return nil
		}
	}
	return nil
}

func (s *memoryListStore[T]) Delete(ctx context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, v := range s.data {
		if s.getID(v) == id {
			s.data = append(s.data[:i], s.data[i+1:]...)
			return nil
		}
	}
	return nil
}

func (s *memoryListStore[T]) DeleteAll(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = nil
	return nil
}

func (s *memoryListStore[T]) Append(ctx context.Context, item T) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = append(s.data, item)
	return nil
}
